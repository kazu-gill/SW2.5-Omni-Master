package game

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
)

// scriptedLLM returns replies in order and records every request.
type scriptedLLM struct {
	replies []llm.Result
	calls   []scriptedCall
}

type scriptedCall struct {
	msgs []llm.Message
	opts llm.CompletionOptions
}

func (s *scriptedLLM) Complete(_ context.Context, msgs []llm.Message, opts llm.CompletionOptions) (llm.Result, error) {
	s.calls = append(s.calls, scriptedCall{msgs: append([]llm.Message(nil), msgs...), opts: opts})
	if len(s.calls) > len(s.replies) {
		return llm.Result{}, errors.New("scriptedLLM: no more replies")
	}
	return s.replies[len(s.calls)-1], nil
}

func newStageEngine(t *testing.T, model ChatModel, retryLimit int) *Engine {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return NewEngine(database, model, nil, retryLimit)
}

func nonEmpty(r llm.Result) error {
	if strings.TrimSpace(r.Text) == "" {
		return errors.New("empty")
	}
	return nil
}

func TestRunStageFirstAttemptOK(t *testing.T) {
	m := &scriptedLLM{replies: []llm.Result{{Text: "ok", FinishReason: "stop"}}}
	e := newStageEngine(t, m, 2)

	res, attempts, err := e.runStage(context.Background(), 1, 1,
		stage{name: "gm", opts: llm.CompletionOptions{MaxTokens: 100}, validate: nonEmpty},
		[]llm.Message{{Role: "user", Content: "x"}})
	if err != nil || res.Text != "ok" || attempts != 1 {
		t.Fatalf("got %q attempts=%d err=%v", res.Text, attempts, err)
	}
	if m.calls[0].opts.Thinking {
		t.Error("first attempt should not think")
	}
}

func TestRunStageRethinksAfterNG(t *testing.T) {
	m := &scriptedLLM{replies: []llm.Result{
		{Text: "", FinishReason: "stop"},
		{Text: "ok", FinishReason: "stop"},
	}}
	e := newStageEngine(t, m, 2)

	res, attempts, err := e.runStage(context.Background(), 1, 1,
		stage{name: "gm", opts: llm.CompletionOptions{MaxTokens: 100}, retryHint: "やり直して", validate: nonEmpty},
		[]llm.Message{{Role: "user", Content: "x"}})
	if err != nil || res.Text != "ok" || attempts != 2 {
		t.Fatalf("got %q attempts=%d err=%v", res.Text, attempts, err)
	}
	retry := m.calls[1]
	if !retry.opts.Thinking {
		t.Error("retry should think")
	}
	if retry.opts.MaxTokens <= 100 {
		t.Errorf("retry MaxTokens = %d, want > 100 for the thinking budget", retry.opts.MaxTokens)
	}
	if len(retry.msgs) != 3 || retry.msgs[1].Role != "assistant" || retry.msgs[2].Role != "user" {
		t.Fatalf("retry messages = %+v", retry.msgs)
	}
	if !strings.Contains(retry.msgs[2].Content, "empty") || !strings.Contains(retry.msgs[2].Content, "やり直して") {
		t.Errorf("retry prompt should carry the error and hint: %q", retry.msgs[2].Content)
	}
}

func TestRunStageGivesUpAfterLimit(t *testing.T) {
	m := &scriptedLLM{replies: []llm.Result{{}, {}, {}, {Text: "too late"}}}
	e := newStageEngine(t, m, 2)

	_, attempts, err := e.runStage(context.Background(), 1, 1,
		stage{name: "gm", validate: nonEmpty}, []llm.Message{{Role: "user", Content: "x"}})
	if err == nil {
		t.Fatal("expected error after retry limit")
	}
	if attempts != 3 || len(m.calls) != 3 {
		t.Errorf("attempts=%d calls=%d, want 3", attempts, len(m.calls))
	}
}

func TestRunStageDoesNotRetryTransportError(t *testing.T) {
	m := &scriptedLLM{} // every call fails
	e := newStageEngine(t, m, 2)

	_, attempts, err := e.runStage(context.Background(), 1, 1,
		stage{name: "gm", validate: nonEmpty}, []llm.Message{{Role: "user", Content: "x"}})
	if err == nil || attempts != 1 {
		t.Fatalf("attempts=%d err=%v, want 1 attempt and an error", attempts, err)
	}
}

func TestRunStageRejectsTruncatedOutput(t *testing.T) {
	m := &scriptedLLM{replies: []llm.Result{
		{Text: "途中で", FinishReason: "length"},
		{Text: "完結した文。", FinishReason: "stop"},
	}}
	e := newStageEngine(t, m, 2)

	res, attempts, err := e.runStage(context.Background(), 1, 1,
		stage{name: "gm", validate: nonEmpty}, []llm.Message{{Role: "user", Content: "x"}})
	if err != nil || attempts != 2 || res.Text != "完結した文。" {
		t.Fatalf("got %q attempts=%d err=%v", res.Text, attempts, err)
	}
}

// ── ターン全体: 直列の呼び出し順と部分失敗 ─────────────────────────────────

func setupSerialEngine(t *testing.T, replies []llm.Result, names ...string) (*Engine, *scriptedLLM, int64) {
	t.Helper()
	m := &scriptedLLM{replies: replies}
	e := newStageEngine(t, m, 2)
	session, _ := db.CreateSession(e.db, "test-quest")
	for _, n := range names {
		_ = db.UpsertNPCSheet(e.db, db.NPCSheet{SessionID: session.ID, Name: n, HP: 30, MP: 15})
	}
	return e, m, session.ID
}

func npcYAML(name string) string {
	return "name: " + name + "\naction: 攻撃\ndialogue: いくぞ！\nmp_cost: 3\nhp_delta: -5"
}

func TestProcessTurnCallsStagesSerially(t *testing.T) {
	e, m, sid := setupSerialEngine(t, []llm.Result{
		{Text: "GMの描写。", FinishReason: "stop"},
		{Text: npcYAML("A"), FinishReason: "stop"},
		{Text: npcYAML("B"), FinishReason: "stop"},
		{Text: `[{"npc_name":"A","hp_delta":-5,"mp_cost":3},{"npc_name":"B","hp_delta":-5,"mp_cost":3}]`, FinishReason: "stop"},
	}, "A", "B")

	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"})
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}
	if len(m.calls) != 4 {
		t.Fatalf("calls = %d, want 4 (GM, NPC A, NPC B, Support)", len(m.calls))
	}
	if !strings.Contains(m.calls[1].msgs[0].Content, "「A」") || !strings.Contains(m.calls[2].msgs[0].Content, "「B」") {
		t.Error("NPC stages should run in sheet order A then B")
	}
	if len(res.NPCActions) != 2 || len(res.Deltas) != 2 {
		t.Errorf("actions=%d deltas=%d", len(res.NPCActions), len(res.Deltas))
	}
	if res.StageAttempts["gm"] != 1 || res.StageAttempts["npc:A"] != 1 || res.StageAttempts["support"] != 1 {
		t.Errorf("StageAttempts = %v", res.StageAttempts)
	}
}

func TestProcessTurnSkipsNPCThatNeverValidates(t *testing.T) {
	bad := llm.Result{Text: "name: A\naction: 攻撃\ndialogue: x\nmp_cost: 99", FinishReason: "stop"}
	e, _, sid := setupSerialEngine(t, []llm.Result{
		{Text: "GMの描写。", FinishReason: "stop"},
		bad, bad, bad,
		{Text: npcYAML("B"), FinishReason: "stop"},
		{Text: `[{"npc_name":"B","hp_delta":-5,"mp_cost":3}]`, FinishReason: "stop"},
	}, "A", "B")

	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"})
	if err != nil {
		t.Fatalf("ProcessTurn should continue without the failed NPC: %v", err)
	}
	if len(res.NPCActions) != 1 || res.NPCActions[0].Name != "B" {
		t.Errorf("NPCActions = %+v, want only B", res.NPCActions)
	}
	if res.StageAttempts["npc:A"] != 3 || len(res.Warnings) != 1 {
		t.Errorf("attempts=%v warnings=%v", res.StageAttempts, res.Warnings)
	}
}

func TestProcessTurnSupportFallsBackAfterRetries(t *testing.T) {
	e, _, sid := setupSerialEngine(t, []llm.Result{
		{Text: "GMの描写。", FinishReason: "stop"},
		{Text: npcYAML("A"), FinishReason: "stop"},
		{Text: "計算しました", FinishReason: "stop"},
		{Text: `[{"npc_name":"Z","hp_delta":0,"mp_cost":0}]`, FinishReason: "stop"},
		{Text: `[]`, FinishReason: "stop"},
	}, "A")

	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"})
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}
	if len(res.Deltas) != 1 || res.Deltas[0].MPCost != 3 || res.Deltas[0].HPDelta != -5 {
		t.Errorf("Deltas = %+v, want fallback to the NPC's own values", res.Deltas)
	}
	if res.StageAttempts["support"] != 3 || len(res.Warnings) != 1 {
		t.Errorf("attempts=%v warnings=%v", res.StageAttempts, res.Warnings)
	}
}

// ── ルールブックの参照 ─────────────────────────────────────────────────────────

func newRAGEngine(t *testing.T, database *sql.DB) *rag.Engine {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": []float32{1, 0, 0}}}})
	}))
	t.Cleanup(srv.Close)
	return rag.New(database, llm.New(srv.URL, "test-embed", 5*time.Second))
}

func TestProcessTurnGivesRulesToGMAndSupport(t *testing.T) {
	m := &scriptedLLM{replies: []llm.Result{
		{Text: "GMの描写。", FinishReason: "stop"},
		{Text: "name: A\naction: エネルギー・ボルト\ndialogue: くらえ\nmp_cost: 5\nhp_delta: 0", FinishReason: "stop"},
		{Text: `[{"npc_name":"A","hp_delta":0,"mp_cost":5}]`, FinishReason: "stop"},
	}}
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := db.InsertRAGChunk(database, db.RAGChunk{
		SourceType: "rulebook", Section: "第三部 データ > 魔法データ > 真語魔法データ", PageFrom: 248, PageTo: 249,
		Text: "【エネルギー・ボルト】消費MP5", Enabled: true, Embedding: []float32{1, 0, 0},
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(database, m, newRAGEngine(t, database), 2)
	session, _ := db.CreateSession(database, "q")
	_ = db.UpsertNPCSheet(database, db.NPCSheet{SessionID: session.ID, Name: "A", HP: 20, MP: 20})

	if _, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: session.ID, Turn: 1, Text: "魔法で攻撃"}); err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}
	gmSystem := m.calls[0].msgs[0].Content
	supportSystem := m.calls[2].msgs[0].Content
	for name, prompt := range map[string]string{"GM": gmSystem, "Support": supportSystem} {
		if !strings.Contains(prompt, "[第三部 データ > 魔法データ > 真語魔法データ p.248-249] 【エネルギー・ボルト】消費MP5") {
			t.Errorf("%s prompt lacks the rule: %q", name, prompt)
		}
		if strings.Count(prompt, "【参照ルール】") != 1 {
			t.Errorf("%s prompt should have exactly one rules header: %q", name, prompt)
		}
	}
}
