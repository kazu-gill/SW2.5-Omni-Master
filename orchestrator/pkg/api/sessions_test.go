package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/game"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// dialSession opens the session's WebSocket and returns a function reading
// messages until one of the given type arrives (others are collected).
func dialSession(t *testing.T, srv *httptest.Server, sessionID int64) func(string) (map[string]any, []map[string]any) {
	t.Helper()
	conn, _, err := websocket.Dial(context.Background(), fmt.Sprintf("ws%s/ws?session_id=%d", srv.URL[4:], sessionID), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return func(want string) (map[string]any, []map[string]any) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var seen []map[string]any
		for {
			var msg map[string]any
			if err := wsjson.Read(ctx, conn, &msg); err != nil {
				t.Fatalf("waiting for %q: %v (seen %v)", want, err, seen)
			}
			if msg["type"] == want {
				return msg, seen
			}
			seen = append(seen, msg)
		}
	}
}

func getState(t *testing.T, srv *httptest.Server, id int64) map[string]any {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/sessions/%d", srv.URL, id))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st map[string]any
	json.NewDecoder(resp.Body).Decode(&st)
	return st
}

func TestTurnRunsInBackgroundAndStreamsProgress(t *testing.T) {
	_, srv, sid := setupHandler(t)
	read := dialSession(t, srv, sid)
	read("busy") // initial state

	resp := postJSON(t, fmt.Sprintf("%s/api/sessions/%d/turn", srv.URL, sid), map[string]string{"text": "扉を開ける"})
	var started map[string]int
	json.NewDecoder(resp.Body).Decode(&started)
	if resp.StatusCode != http.StatusAccepted || started["turn"] != 1 {
		t.Fatalf("status=%d body=%v", resp.StatusCode, started)
	}

	msg, seen := read("turn")
	result := msg["result"].(map[string]any)
	if result["input"] != "扉を開ける" || result["gm_narration"] == "" || result["mode"] != "action" {
		t.Errorf("result = %v", result)
	}
	var stages, deltas int
	for _, m := range seen {
		switch m["type"] {
		case "stage":
			stages++
		case "delta":
			deltas++
		}
	}
	if stages < 6 || deltas == 0 {
		t.Errorf("expected stage and delta messages before the turn, got %d stages / %d deltas", stages, deltas)
	}
	read("busy") // released

	st := getState(t, srv, sid)
	if st["session"].(map[string]any)["turn"] != float64(1) || len(st["turns"].([]any)) != 1 || st["busy"] != false {
		t.Errorf("state = %v", st)
	}

	// The next turn number comes from the server.
	resp = postJSON(t, fmt.Sprintf("%s/api/sessions/%d/turn", srv.URL, sid), map[string]string{"text": "質問", "mode": "gm"})
	json.NewDecoder(resp.Body).Decode(&started)
	if started["turn"] != 2 {
		t.Errorf("second turn = %v", started)
	}
	msg, _ = read("turn")
	if r := msg["result"].(map[string]any); r["mode"] != "gm" || len(r["npc_actions"].([]any)) != 0 || r["checks"] == nil {
		t.Errorf("GM appeal result = %v", r)
	}
}

func TestSecondTurnWhileBusyIsRejected(t *testing.T) {
	h, srv, sid := setupHandler(t)
	h.turns.running[sid] = &runningTurn{turn: 1, cancel: func() {}}
	resp := postJSON(t, fmt.Sprintf("%s/api/sessions/%d/turn", srv.URL, sid), map[string]string{"text": "x"})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
	delete(h.turns.running, sid)
}

func TestRestoreAndRedo(t *testing.T) {
	h, srv, sid := setupHandler(t)
	read := dialSession(t, srv, sid)
	for _, text := range []string{"一", "二"} {
		postJSON(t, fmt.Sprintf("%s/api/sessions/%d/turn", srv.URL, sid), map[string]string{"text": text})
		read("turn")
		read("busy")
	}
	// Damage the NPC after the fact, then restore to before turn 2.
	sheets, _ := db.GetNPCSheets(h.database, sid)
	sheets[0].HP = 1
	_ = db.UpsertNPCSheet(h.database, sheets[0])
	resp := postJSON(t, fmt.Sprintf("%s/api/sessions/%d/restore", srv.URL, sid), map[string]int{"turn": 2})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("restore status = %d", resp.StatusCode)
	}
	st := getState(t, srv, sid)
	npc := st["npc_sheets"].([]any)[0].(map[string]any)
	if len(st["turns"].([]any)) != 1 || npc["hp"] != float64(30) {
		t.Errorf("after restore: turns=%d npc=%v", len(st["turns"].([]any)), npc)
	}

	// Redo turn 1 with a new input.
	resp = postJSON(t, fmt.Sprintf("%s/api/sessions/%d/redo", srv.URL, sid), map[string]any{"turn": 1, "text": "やり直し"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("redo status = %d", resp.StatusCode)
	}
	msg, _ := read("turn")
	if r := msg["result"].(map[string]any); r["turn"] != float64(1) || r["input"] != "やり直し" {
		t.Errorf("redo result = %v", r)
	}
}

func TestSessionsCreateListAndQuest(t *testing.T) {
	h, srv, _ := setupHandler(t)
	_ = db.SeedQuests(h.database)
	quests, _ := db.ListQuests(h.database)
	resp := postJSON(t, srv.URL+"/api/sessions", map[string]any{"title": "新しい冒険", "quest_id": quests[0].ID})
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	if resp.StatusCode != http.StatusCreated || created["title"] != "新しい冒険" || created["quest_id"] != float64(quests[0].ID) {
		t.Fatalf("status=%d created=%v", resp.StatusCode, created)
	}
	list, _ := http.Get(srv.URL + "/api/sessions")
	var sessions []map[string]any
	json.NewDecoder(list.Body).Decode(&sessions)
	if len(sessions) != 2 || sessions[0]["quest_title"] != quests[0].Title {
		t.Errorf("sessions = %v", sessions)
	}
}

func TestNPCLimitAndErrorsAreJSON(t *testing.T) {
	h, srv, sid := setupHandler(t)
	for _, n := range []string{"B", "C"} {
		_ = db.UpsertNPCSheet(h.database, db.NPCSheet{SessionID: sid, Name: n, HP: 10})
	}
	resp := postJSON(t, srv.URL+"/api/npc-sheet", map[string]any{"session_id": sid, "persona_id": "gard"})
	var e map[string]string
	json.NewDecoder(resp.Body).Decode(&e)
	if resp.StatusCode != http.StatusBadRequest || e["error"] == "" {
		t.Errorf("4th NPC should be refused with a JSON error: status=%d body=%v", resp.StatusCode, e)
	}
	_ = db.DeleteNPCSheet(h.database, sid, "C")
	resp = postJSON(t, srv.URL+"/api/npc-sheet", map[string]any{"session_id": sid, "persona_id": "gard"})
	sheets, _ := db.GetNPCSheets(h.database, sid)
	var gard *db.NPCSheet
	for i := range sheets {
		if sheets[i].Name == "ガルド" {
			gard = &sheets[i]
		}
	}
	if resp.StatusCode != http.StatusNoContent || gard == nil || gard.HPMax != 38 {
		t.Errorf("adding the 3rd NPC: status=%d sheet=%+v", resp.StatusCode, gard)
	}
}

func TestSceneRequestCarriesTheStorySoFar(t *testing.T) {
	h, _, sid := setupHandler(t)
	for turn, narr := range []string{"", "一行は坑道に入った。", "ゴブリンが飛び出してきた。"} {
		if turn == 0 {
			continue
		}
		b, _ := json.Marshal(game.TurnResult{Turn: turn, Input: "進む", GMNarration: narr})
		_ = db.SaveTurnResult(h.database, sid, turn, string(b))
	}
	_ = db.AddSummary(h.database, sid, 1, []string{"ギルたちは鉱山の坑道へ向かった。"})

	got := h.sceneRequest(sid, 2, &game.TurnResult{Turn: 2, Input: "斬りかかる", GMNarration: "ゴブリンが飛び出してきた。"})
	for _, want := range []string{"これまで: ギルたちは鉱山の坑道へ向かった。", "直前の場面: 一行は坑道に入った。", "プレイヤーの行動: 斬りかかる", "この場面: ゴブリンが飛び出してきた。"} {
		if !strings.Contains(got, want) {
			t.Errorf("request lacks %q:\n%s", want, got)
		}
	}
}

// ── 止まったターン ─────────────────────────────────────────────────────────

// stageLLM is the fake LLM of setupHandler with control over each chat
// call (1-based, in the order they arrive): a call in fail drops the
// connection, a call in hold waits until release is closed (or the request is
// cancelled) and tells entered when it starts waiting.
type stageLLM struct {
	srv     *httptest.Server
	mu      sync.Mutex
	calls   int
	fail    map[int]bool
	hold    map[int]bool
	entered chan int
	release chan struct{}
	once    sync.Once
}

// let lets the held calls go on.
func (m *stageLLM) let() { m.once.Do(func() { close(m.release) }) }

func newStageLLM(t *testing.T) *stageLLM {
	t.Helper()
	inner := mockStageLLM()
	t.Cleanup(inner.Close)
	target, _ := url.Parse(inner.URL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	m := &stageLLM{fail: map[int]bool{}, hold: map[int]bool{}, entered: make(chan int, 8), release: make(chan struct{})}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			proxy.ServeHTTP(w, r)
			return
		}
		m.mu.Lock()
		m.calls++
		n, fail, hold := m.calls, m.fail[m.calls], m.hold[m.calls]
		m.mu.Unlock()
		switch {
		case fail:
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		case hold:
			m.entered <- n
			select {
			case <-m.release:
			case <-r.Context().Done():
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(m.srv.Close)
	t.Cleanup(m.let) // runs first: a failed test leaves no call held
	return m
}

func (m *stageLLM) set(fail, hold []int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range fail {
		m.fail[n] = true
	}
	for _, n := range hold {
		m.hold[n] = true
	}
}

func (m *stageLLM) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *stageLLM) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-m.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the LLM call to hold never came")
	}
}

// LLM calls of a turn with one NPC: GM, ガルド, support.
const callGM, callNPC, callSupport = 1, 2, 3

func post(t *testing.T, srv *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	resp := postJSON(t, srv.URL+path, body)
	resp.Body.Close()
	return resp
}

func doJSON(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

// stopAtNPC starts a turn whose NPC call drops the connection and waits for
// the error and the release of the session.
func stopAtNPC(t *testing.T, m *stageLLM, srv *httptest.Server, sid int64, read func(string) (map[string]any, []map[string]any)) map[string]any {
	t.Helper()
	m.set([]int{m.count() + callNPC}, nil)
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/turn", sid), map[string]string{"text": "扉を開ける"}); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("turn status = %d", resp.StatusCode)
	}
	msg, _ := read("error")
	read("busy")
	return msg
}

// party is the NPC sheets of a state without ids and times, which a restore
// renews.
func party(st map[string]any) string {
	var out []string
	for _, x := range st["npc_sheets"].([]any) {
		n := x.(map[string]any)
		out = append(out, fmt.Sprint(n["name"], n["hp"], n["mp"], n["lane"], n["yaml_blob"]))
	}
	return strings.Join(out, ";")
}

func stoppedTurn(t *testing.T, st map[string]any) map[string]any {
	t.Helper()
	s, _ := st["stopped_turn"].(map[string]any)
	return s
}

func TestTurnFailureKeepsProgress(t *testing.T) {
	m := newStageLLM(t)
	_, srv, sid := setupHandlerLLM(t, m.srv.URL)
	read := dialSession(t, srv, sid)
	read("busy")
	before := getState(t, srv, sid)

	msg := stopAtNPC(t, m, srv, sid, read)
	if msg["resumable"] != true || msg["turn"] != float64(1) || !strings.Contains(msg["message"].(string), "続きから再開できます") {
		t.Errorf("error = %v, want resumable turn 1", msg)
	}
	st := getState(t, srv, sid)
	s := stoppedTurn(t, st)
	if s == nil {
		t.Fatalf("stopped_turn = %v", st["stopped_turn"])
	}
	done, _ := s["done"].([]any)
	if len(done) != 1 || done[0] != "gm" || s["turn"] != float64(1) || s["input"] != "扉を開ける" || s["mode"] != "action" || s["error"] == "" {
		t.Errorf("stopped_turn = %v", s)
	}
	if r, _ := s["result"].(map[string]any); r["gm_narration"] != "GMが状況を描写します。" {
		t.Errorf("stopped_turn.result = %v", s["result"])
	}
	if fmt.Sprint(st["npc_sheets"]) != fmt.Sprint(before["npc_sheets"]) || len(st["turns"].([]any)) != 0 {
		t.Errorf("after the stop: npc_sheets=%v (before %v) turns=%v", st["npc_sheets"], before["npc_sheets"], st["turns"])
	}
}

func TestResumeEndpointCompletesTurn(t *testing.T) {
	m := newStageLLM(t)
	_, srv, sid := setupHandlerLLM(t, m.srv.URL)
	read := dialSession(t, srv, sid)
	read("busy")
	stopAtNPC(t, m, srv, sid, read)

	resp := postJSON(t, fmt.Sprintf("%s/api/sessions/%d/resume", srv.URL, sid), nil)
	var started map[string]int
	json.NewDecoder(resp.Body).Decode(&started)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || started["turn"] != 1 {
		t.Fatalf("resume: status=%d body=%v", resp.StatusCode, started)
	}
	msg, _ := read("turn")
	if r := msg["result"].(map[string]any); r["turn"] != float64(1) || r["input"] != "扉を開ける" || len(r["npc_actions"].([]any)) != 1 {
		t.Errorf("resumed result = %v", r)
	}
	read("busy")
	st := getState(t, srv, sid)
	if st["stopped_turn"] != nil || len(st["turns"].([]any)) != 1 {
		t.Errorf("after resume: stopped_turn=%v turns=%v", st["stopped_turn"], st["turns"])
	}

	// Nothing left to resume.
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/resume", sid), nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("second resume status = %d, want 404", resp.StatusCode)
	}
}

func TestDiscardEndpoint(t *testing.T) {
	m := newStageLLM(t)
	_, srv, sid := setupHandlerLLM(t, m.srv.URL)
	read := dialSession(t, srv, sid)
	read("busy")
	post(t, srv, fmt.Sprintf("/api/sessions/%d/turn", sid), map[string]string{"text": "一"})
	read("turn")
	read("busy")
	before := getState(t, srv, sid)

	stopAtNPC(t, m, srv, sid, read)
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/discard", sid), nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("discard status = %d", resp.StatusCode)
	}
	st := getState(t, srv, sid)
	if st["stopped_turn"] != nil || fmt.Sprint(st["turns"]) != fmt.Sprint(before["turns"]) ||
		party(st) != party(before) || fmt.Sprint(st["session"].(map[string]any)["turn"]) != "1" {
		t.Errorf("after discard: %v\nbefore: %v", st, before)
	}
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/discard", sid), nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("discard with nothing stopped: status = %d, want 404", resp.StatusCode)
	}
	// The next turn is turn 2 again.
	resp := postJSON(t, fmt.Sprintf("%s/api/sessions/%d/turn", srv.URL, sid), map[string]string{"text": "二"})
	var started map[string]int
	json.NewDecoder(resp.Body).Decode(&started)
	resp.Body.Close()
	if started["turn"] != 2 {
		t.Errorf("turn after discard = %v", started)
	}
	read("turn")
	read("busy")
}

func TestEditsRefusedWhileStopped(t *testing.T) {
	m := newStageLLM(t)
	h, srv, sid := setupHandlerLLM(t, m.srv.URL)
	_ = db.SeedQuests(h.database)
	quests, _ := db.ListQuests(h.database)
	pc, _ := db.CreatePlayerCharacter(h.database, "PC", "{}")
	sheets, _ := db.GetNPCSheets(h.database, sid)
	other, _ := db.CreateSession(h.database, "")
	read := dialSession(t, srv, sid)
	read("busy")
	stopAtNPC(t, m, srv, sid, read)

	type call struct {
		name, method, path string
		body               any
	}
	calls := []call{
		{"new turn", http.MethodPost, fmt.Sprintf("/api/sessions/%d/turn", sid), map[string]string{"text": "x"}},
		{"redo", http.MethodPost, fmt.Sprintf("/api/sessions/%d/redo", sid), map[string]any{"turn": 1}},
		{"restore", http.MethodPost, fmt.Sprintf("/api/sessions/%d/restore", sid), map[string]any{"turn": 1}},
		{"session quest", http.MethodPost, fmt.Sprintf("/api/sessions/%d/quest", sid), map[string]any{"quest_id": quests[0].ID}},
		{"npc-position", http.MethodPatch, "/api/npc-position", map[string]any{"session_id": sid, "name": "ガルド", "lane": "party"}},
		{"npc-sheet add", http.MethodPost, "/api/npc-sheet", map[string]any{"session_id": sid, "persona_id": "gard"}},
		{"npc-sheet delete", http.MethodDelete, "/api/npc-sheet", map[string]any{"session_id": sid, "name": "ガルド"}},
		{"npc-sheet update", http.MethodPatch, fmt.Sprintf("/api/npc-sheet/%d", sheets[0].ID), map[string]any{"hp": 1, "mp": 1}},
		{"quest accept", http.MethodPatch, fmt.Sprintf("/api/quests/%d/accept", quests[0].ID), nil},
		{"quest complete", http.MethodPatch, fmt.Sprintf("/api/quests/%d/complete", quests[0].ID), nil},
		{"pc update", http.MethodPut, fmt.Sprintf("/api/player-characters/%d", pc.ID), map[string]any{"name": "PC2", "json_blob": "{}"}},
		{"pc delete", http.MethodDelete, fmt.Sprintf("/api/player-characters/%d", pc.ID), nil},
		{"pc activate", http.MethodPatch, fmt.Sprintf("/api/player-characters/%d/activate", pc.ID), nil},
		{"pc deactivate", http.MethodPatch, fmt.Sprintf("/api/player-characters/%d/deactivate", pc.ID), nil},
	}
	for _, c := range calls {
		if resp := doJSON(t, c.method, srv.URL+c.path, c.body); resp.StatusCode != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409", c.name, resp.StatusCode)
		}
	}
	resp := postJSON(t, fmt.Sprintf("%s/api/sessions/%d/turn", srv.URL, sid), map[string]string{"text": "x"})
	var e map[string]string
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if e["error"] != "止まったターンがあります。「続きから」か「取り消す」を先に選んでください" {
		t.Errorf("error = %q", e["error"])
	}

	// Another session is not stopped: its own edits go through.
	if resp := doJSON(t, http.MethodPost, srv.URL+"/api/npc-sheet", map[string]any{"session_id": other.ID, "persona_id": "gard"}); resp.StatusCode != http.StatusNoContent {
		t.Errorf("other session's npc-sheet add: status = %d", resp.StatusCode)
	}
	// Creating a PC changes nothing in play.
	if resp := doJSON(t, http.MethodPost, srv.URL+"/api/player-characters", map[string]any{"name": "新"}); resp.StatusCode != http.StatusCreated {
		t.Errorf("pc create: status = %d", resp.StatusCode)
	}
	if st := getState(t, srv, sid); stoppedTurn(t, st) == nil || len(st["npc_sheets"].([]any)) != 1 {
		t.Errorf("state changed while stopped: %v", st)
	}
}

func TestResumeRefusedWhenPartyChanged(t *testing.T) {
	m := newStageLLM(t)
	h, srv, sid := setupHandlerLLM(t, m.srv.URL)
	read := dialSession(t, srv, sid)
	read("busy")
	stopAtNPC(t, m, srv, sid, read)
	_ = db.UpsertNPCSheet(h.database, db.NPCSheet{SessionID: sid, Name: "ベル", HP: 10}) // behind the API's back

	resp := postJSON(t, fmt.Sprintf("%s/api/sessions/%d/resume", srv.URL, sid), nil)
	var e map[string]string
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || e["error"] != game.ErrPartyChanged.Error() {
		t.Errorf("resume: status=%d body=%v, want 409 ErrPartyChanged", resp.StatusCode, e)
	}
	if stoppedTurn(t, getState(t, srv, sid)) == nil {
		t.Error("the stopped turn should stay for discard")
	}
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/discard", sid), nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("discard status = %d", resp.StatusCode)
	}
}

func TestResumeTwiceIsBusy(t *testing.T) {
	m := newStageLLM(t)
	_, srv, sid := setupHandlerLLM(t, m.srv.URL)
	read := dialSession(t, srv, sid)
	read("busy")
	stopAtNPC(t, m, srv, sid, read)

	m.set(nil, []int{m.count() + 1}) // the resumed NPC call waits
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/resume", sid), nil); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("first resume status = %d", resp.StatusCode)
	}
	m.waitEntered(t)
	resp := postJSON(t, fmt.Sprintf("%s/api/sessions/%d/resume", srv.URL, sid), nil)
	var e map[string]string
	json.NewDecoder(resp.Body).Decode(&e)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || e["error"] != errBusy.Error() {
		t.Errorf("second resume: status=%d body=%v, want 409 busy", resp.StatusCode, e)
	}
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/discard", sid), nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("discard while resuming: status = %d, want 409", resp.StatusCode)
	}
	m.let()
	read("turn")
	read("busy")
	if st := getState(t, srv, sid); st["stopped_turn"] != nil || len(st["turns"].([]any)) != 1 {
		t.Errorf("after resume: %v", st)
	}
}

func TestCancelKeepsProgress(t *testing.T) {
	m := newStageLLM(t)
	_, srv, sid := setupHandlerLLM(t, m.srv.URL)
	read := dialSession(t, srv, sid)
	read("busy")
	m.set(nil, []int{callNPC})
	post(t, srv, fmt.Sprintf("/api/sessions/%d/turn", sid), map[string]string{"text": "扉を開ける"})
	m.waitEntered(t)
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/cancel", sid), nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel status = %d", resp.StatusCode)
	}
	msg, _ := read("error")
	read("busy")
	if msg["resumable"] != true || !strings.HasPrefix(msg["message"].(string), "ターンを中断しました。途中まで残っています") {
		t.Errorf("error = %v", msg)
	}
	s := stoppedTurn(t, getState(t, srv, sid))
	if done, _ := s["done"].([]any); len(done) != 1 || done[0] != "gm" {
		t.Errorf("stopped_turn = %v", s)
	}
}

func TestStartupStopsRunningTurns(t *testing.T) {
	m := newStageLLM(t)
	h, srv, sid := setupHandlerLLM(t, m.srv.URL)
	read := dialSession(t, srv, sid)
	read("busy")
	stopAtNPC(t, m, srv, sid, read)
	// As if the process died mid-turn: the row is still running.
	p, _ := db.GetTurnProgress(h.database, sid)
	p.Status = db.ProgressRunning
	if err := db.SaveTurnProgress(h.database, *p); err != nil {
		t.Fatal(err)
	}
	if st := getState(t, srv, sid); st["stopped_turn"] != nil {
		t.Fatalf("a running row is not a stopped turn: %v", st["stopped_turn"])
	}

	if n, err := db.StopRunningTurns(h.database); err != nil || n != 1 {
		t.Fatalf("StopRunningTurns = %d, %v", n, err)
	}
	if stoppedTurn(t, getState(t, srv, sid)) == nil {
		t.Fatal("stopped_turn should appear after startup")
	}
	if resp := post(t, srv, fmt.Sprintf("/api/sessions/%d/resume", sid), nil); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("resume status = %d", resp.StatusCode)
	}
	read("turn")
	read("busy")
	if st := getState(t, srv, sid); st["stopped_turn"] != nil || len(st["turns"].([]any)) != 1 {
		t.Errorf("after resume: %v", st)
	}
}

func TestFailureBeforeProgressRestores(t *testing.T) {
	h, srv, sid := setupHandler(t)
	read := dialSession(t, srv, sid)
	read("busy")
	// No checkpoint can be saved: the turn fails before its progress row.
	if _, err := h.database.Exec(`DROP TABLE checkpoints`); err != nil {
		t.Skip("cannot break the checkpoint table:", err)
	}
	post(t, srv, fmt.Sprintf("/api/sessions/%d/turn", sid), map[string]string{"text": "x"})
	msg, _ := read("error")
	if msg["resumable"] == true || !strings.Contains(msg["message"].(string), "開始前に戻っています") {
		t.Errorf("error = %v, want a non-resumable restore", msg)
	}
	if st := getState(t, srv, sid); st["stopped_turn"] != nil {
		t.Errorf("stopped_turn = %v", st["stopped_turn"])
	}
}
