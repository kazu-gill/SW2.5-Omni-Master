package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
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
	if r := msg["result"].(map[string]any); r["mode"] != "gm" || r["npc_actions"] != nil {
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
