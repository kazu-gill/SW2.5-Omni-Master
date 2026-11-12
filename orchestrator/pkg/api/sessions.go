package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/game"
)

// writeError sends {"error": msg}. Messages are for the player: say what went
// wrong and what to do next.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

// ── ターンの実行管理 ─────────────────────────────────────────────────────────

// turnRunner runs at most one turn per session in the background. A turn
// takes most of a minute, so requests return at once and progress goes out
// over the WebSocket.
type turnRunner struct {
	mu      sync.Mutex
	running map[int64]*runningTurn
}

type runningTurn struct {
	turn   int
	cancel context.CancelFunc
}

func newTurnRunner() *turnRunner {
	return &turnRunner{running: map[int64]*runningTurn{}}
}

func (tr *turnRunner) busy(sessionID int64) bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.running[sessionID] != nil
}

var errBusy = errors.New("このセッションではターンを処理中です。終わるまで待つか、中断してください。")

// startTurn reserves the session, then runs the turn in the background.
// prepare runs under the reservation and returns the turn number; it is where
// a redo rewinds the session first.
func (h *Handler) startTurn(sessionID int64, text, mode string, prepare func() (int, error)) (int, error) {
	tr := h.turns
	tr.mu.Lock()
	if tr.running[sessionID] != nil {
		tr.mu.Unlock()
		return 0, errBusy
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.turnTimeout)
	run := &runningTurn{cancel: cancel}
	tr.running[sessionID] = run
	tr.mu.Unlock()

	release := func() {
		cancel()
		tr.mu.Lock()
		delete(tr.running, sessionID)
		tr.mu.Unlock()
		h.send(sessionID, map[string]any{"type": "busy", "busy": false})
	}
	turn, err := prepare()
	if err != nil {
		release()
		return 0, err
	}
	run.turn = turn
	h.send(sessionID, map[string]any{"type": "busy", "busy": true})

	go func() {
		defer release()
		input := game.PlayerInput{SessionID: sessionID, Turn: turn, Text: text, Mode: mode}
		ev := wsEvents{h: h, sessionID: sessionID}
		result, err := h.engine.ProcessTurn(ctx, input, ev)
		if err != nil {
			// Nothing of a failed turn stays: put the session back to its start.
			if rerr := db.RestoreBeforeTurn(h.database, sessionID, turn); rerr != nil {
				_ = db.DeleteTurnsFrom(h.database, sessionID, turn)
			}
			h.send(sessionID, map[string]any{"type": "error", "turn": turn, "message": turnErrorMessage(ctx, err)})
			return
		}
		b, _ := json.Marshal(result)
		if err := db.SaveTurnResult(h.database, sessionID, turn, string(b)); err != nil {
			log.Printf("[turn] save failed: %v", err)
		}
		_ = db.TouchSession(h.database, sessionID)
		h.send(sessionID, map[string]any{"type": "turn", "result": result})

		// The summary runs while the session is still reserved, so the next
		// turn starts with it in place.
		if _, err := h.engine.MaybeSummarize(ctx, sessionID, ev); err != nil {
			log.Printf("[summary] session %d: %v", sessionID, err)
		}
		if h.images != nil && mode == game.ModeAction {
			go h.makeSceneImage(sessionID, turn, result)
		}
	}()
	return turn, nil
}

func turnErrorMessage(ctx context.Context, err error) string {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return "ターンを中断しました。状態はターンの開始前に戻っています。"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "ターンが時間内に終わらなかったため中止しました。状態は開始前に戻っています。LLM が混み合っていないか MLX ダッシュボードで確認してください。"
	case strings.Contains(err.Error(), "connection refused"):
		return "LLM に接続できません。MLX ダッシュボードでチャット用モデルを起動してから、もう一度送ってください。"
	}
	return "ターンを処理できませんでした（状態は開始前に戻っています）: " + err.Error()
}

// wsEvents forwards a turn's progress to the session's WebSocket clients.
type wsEvents struct {
	h         *Handler
	sessionID int64
}

func (e wsEvents) Stage(s game.StageEvent) {
	e.h.send(e.sessionID, map[string]any{
		"type": "stage", "turn": s.Turn, "stage": s.Stage, "label": s.Label,
		"status": s.Status, "attempt": s.Attempt, "message": s.Message,
	})
}

func (e wsEvents) Delta(turn int, stage, text string) {
	e.h.send(e.sessionID, map[string]any{"type": "delta", "turn": turn, "stage": stage, "text": text})
}

func (e wsEvents) Rules(turn int, stage string, hits []game.RuleHit) {
	e.h.send(e.sessionID, map[string]any{"type": "rules", "turn": turn, "stage": stage, "rules": hits})
}

func (e wsEvents) Check(turn int, c game.Check) {
	e.h.send(e.sessionID, map[string]any{"type": "check", "turn": turn, "check": c})
}

// ── セッション API ───────────────────────────────────────────────────────────

type sessionJSON struct {
	db.Session
	QuestID *int64 `json:"quest_id"`
	Turn    int    `json:"turn"`
}

func (h *Handler) sessionJSON(s db.Session) sessionJSON {
	out := sessionJSON{Session: s}
	if id, err := strconv.ParseInt(s.QuestID, 10, 64); err == nil {
		out.QuestID = &id
	}
	out.Turn, _ = db.LatestTurn(h.database, s.ID)
	return out
}

type npcSheetJSON struct {
	db.NPCSheet
	Lane string `json:"lane"`
}

func npcSheetsJSON(sheets []db.NPCSheet) []npcSheetJSON {
	out := make([]npcSheetJSON, 0, len(sheets))
	for _, s := range sheets {
		out = append(out, npcSheetJSON{NPCSheet: s, Lane: laneOf(s.PositionY)})
	}
	return out
}

func laneOf(y int) string {
	switch {
	case y <= 2:
		return "enemy"
	case y <= 5:
		return "front"
	default:
		return "party"
	}
}

var laneY = map[string]int{"enemy": 1, "front": 4, "party": 7}

func (h *Handler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := db.ListSessions(h.database)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type summary struct {
		sessionJSON
		QuestTitle string `json:"quest_title"`
		LastLine   string `json:"last_line"`
	}
	out := make([]summary, 0, len(sessions))
	for _, s := range sessions {
		sj := h.sessionJSON(s)
		item := summary{sessionJSON: sj}
		if sj.QuestID != nil {
			if q, err := db.GetQuest(h.database, *sj.QuestID); err == nil {
				item.QuestTitle = q.Title
			}
		}
		if turns, err := db.ListTurnResults(h.database, s.ID); err == nil && len(turns) > 0 {
			var last game.TurnResult
			if json.Unmarshal([]byte(turns[len(turns)-1]), &last) == nil {
				item.LastLine = truncateRunes(last.GMNarration, 80)
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func truncateRunes(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (h *Handler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title   string `json:"title"`
		QuestID *int64 `json:"quest_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "リクエストの形式が正しくありません")
		return
	}
	questID := ""
	if req.QuestID != nil {
		if _, err := db.GetQuest(h.database, *req.QuestID); err != nil {
			writeError(w, http.StatusBadRequest, "指定したクエストが見つかりません")
			return
		}
		questID = strconv.FormatInt(*req.QuestID, 10)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "セッション " + time.Now().Format("01/02 15:04")
	}
	s, err := db.CreateSessionTitled(h.database, title, questID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, h.sessionJSON(*s))
}

func (h *Handler) loadSession(w http.ResponseWriter, r *http.Request) (*db.Session, bool) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "セッション ID が正しくありません")
		return nil, false
	}
	s, err := db.GetSession(h.database, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "セッションが見つかりません。一覧から選び直してください。")
		return nil, false
	}
	return s, true
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	sj := h.sessionJSON(*s)
	state := map[string]any{"session": sj, "busy": h.turns.busy(s.ID)}

	state["quest"] = nil
	if sj.QuestID != nil {
		if q, err := db.GetQuest(h.database, *sj.QuestID); err == nil {
			state["quest"] = q
		}
	}
	state["pc"] = nil
	if pc, err := db.GetActivePlayerCharacter(h.database); err == nil && pc != nil {
		state["pc"] = pcJSON(*pc)
	}
	sheets, _ := db.GetNPCSheets(h.database, s.ID)
	state["npc_sheets"] = npcSheetsJSON(sheets)

	turns := []json.RawMessage{}
	if raws, err := db.ListTurnResults(h.database, s.ID); err == nil {
		for _, raw := range raws {
			turns = append(turns, json.RawMessage(raw))
		}
	}
	state["turns"] = turns

	type cpJSON struct {
		Turn      int       `json:"turn"`
		CreatedAt time.Time `json:"created_at"`
	}
	cps := []cpJSON{}
	if list, err := db.ListCheckpoints(h.database, s.ID); err == nil {
		seen := map[int]bool{}
		for _, cp := range list {
			if !seen[cp.Turn] {
				seen[cp.Turn] = true
				cps = append(cps, cpJSON{Turn: cp.Turn, CreatedAt: cp.CreatedAt})
			}
		}
	}
	state["checkpoints"] = cps
	summary, _ := db.ListSummary(h.database, s.ID)
	if summary == nil {
		summary = []string{}
	}
	state["summary"] = summary
	writeJSON(w, http.StatusOK, state)
}

func (h *Handler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	if h.turns.busy(s.ID) {
		writeError(w, http.StatusConflict, errBusy.Error())
		return
	}
	if err := db.DeleteSession(h.database, s.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSessionTurn(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	var req struct {
		Text string `json:"text"`
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "行動か質問を入力してください")
		return
	}
	mode, ok := validMode(req.Mode)
	if !ok {
		writeError(w, http.StatusBadRequest, `mode は "action" か "gm" です`)
		return
	}
	turn, err := h.startTurn(s.ID, req.Text, mode, func() (int, error) {
		latest, err := db.LatestTurn(h.database, s.ID)
		return latest + 1, err
	})
	h.respondStarted(w, turn, err)
}

func validMode(m string) (string, bool) {
	switch m {
	case "", game.ModeAction:
		return game.ModeAction, true
	case game.ModeGM:
		return game.ModeGM, true
	}
	return "", false
}

func (h *Handler) respondStarted(w http.ResponseWriter, turn int, err error) {
	switch {
	case errors.Is(err, errBusy):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		writeJSON(w, http.StatusAccepted, map[string]int{"turn": turn})
	}
}

func (h *Handler) handleCancelTurn(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	h.turns.mu.Lock()
	run := h.turns.running[s.ID]
	h.turns.mu.Unlock()
	if run != nil {
		run.cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRedo rewinds to the start of a turn and plays it again, with the same
// input or a new one.
func (h *Handler) handleRedo(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	var req struct {
		Turn int     `json:"turn"`
		Text *string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Turn < 1 {
		writeError(w, http.StatusBadRequest, "やり直すターンを指定してください")
		return
	}
	prev, err := h.turnResult(s.ID, req.Turn)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("ターン %d が見つかりません", req.Turn))
		return
	}
	text := prev.Input
	if req.Text != nil && strings.TrimSpace(*req.Text) != "" {
		text = *req.Text
	}
	turn, err := h.startTurn(s.ID, text, prev.Mode, func() (int, error) {
		if err := db.RestoreBeforeTurn(h.database, s.ID, req.Turn); err != nil {
			return 0, fmt.Errorf("ターン %d の開始時点に戻せませんでした（チェックポイントは最新 10 件だけ残ります）: %w", req.Turn, err)
		}
		return req.Turn, nil
	})
	h.respondStarted(w, turn, err)
}

func (h *Handler) turnResult(sessionID int64, turn int) (*game.TurnResult, error) {
	raws, err := db.ListTurnResults(h.database, sessionID)
	if err != nil {
		return nil, err
	}
	for _, raw := range raws {
		var tr game.TurnResult
		if json.Unmarshal([]byte(raw), &tr) == nil && tr.Turn == turn {
			if tr.Mode == "" {
				tr.Mode = game.ModeAction
			}
			return &tr, nil
		}
	}
	return nil, fmt.Errorf("turn %d not found", turn)
}

func (h *Handler) handleRestore(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	var req struct {
		Turn int `json:"turn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Turn < 1 {
		writeError(w, http.StatusBadRequest, "戻すターンを指定してください")
		return
	}
	if h.turns.busy(s.ID) {
		writeError(w, http.StatusConflict, errBusy.Error())
		return
	}
	if err := db.RestoreBeforeTurn(h.database, s.ID, req.Turn); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("ターン %d の開始時点のチェックポイントがありません（最新 10 件だけ残ります）", req.Turn))
		return
	}
	_ = db.TouchSession(h.database, s.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSessionQuest(w http.ResponseWriter, r *http.Request) {
	s, ok := h.loadSession(w, r)
	if !ok {
		return
	}
	var req struct {
		QuestID int64 `json:"quest_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.QuestID == 0 {
		writeError(w, http.StatusBadRequest, "受注するクエストを指定してください")
		return
	}
	if _, err := db.GetQuest(h.database, req.QuestID); err != nil {
		writeError(w, http.StatusNotFound, "クエストが見つかりません")
		return
	}
	_ = db.AcceptQuest(h.database, req.QuestID)
	if err := db.SetSessionQuest(h.database, s.ID, strconv.FormatInt(req.QuestID, 10)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pcJSON is a player character with its sheet as JSON (not a string).
func pcJSON(pc db.PlayerCharacter) map[string]any {
	var sheet any
	_ = json.Unmarshal([]byte(pc.JSONBlob), &sheet)
	return map[string]any{"id": pc.ID, "name": pc.Name, "is_active": pc.IsActive, "sheet": sheet, "json_blob": pc.JSONBlob}
}
