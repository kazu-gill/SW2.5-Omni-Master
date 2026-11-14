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

// scriptedLLM returns replies in order and records every request. A call
// whose 1-based number is in failAt returns a transport error instead and
// does not use up a reply.
type scriptedLLM struct {
	replies []llm.Result
	calls   []scriptedCall
	failAt  map[int]bool
	next    int
}

type scriptedCall struct {
	msgs []llm.Message
	opts llm.CompletionOptions
}

func (s *scriptedLLM) Complete(_ context.Context, msgs []llm.Message, opts llm.CompletionOptions) (llm.Result, error) {
	s.calls = append(s.calls, scriptedCall{msgs: append([]llm.Message(nil), msgs...), opts: opts})
	if s.failAt[len(s.calls)] {
		return llm.Result{}, errors.New("scriptedLLM: connection refused")
	}
	if s.next >= len(s.replies) {
		return llm.Result{}, errors.New("scriptedLLM: no more replies")
	}
	s.next++
	return s.replies[s.next-1], nil
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

	res, attempts, err := e.runStage(context.Background(), stageRun{sessionID: 1, turn: 1},
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

	res, attempts, err := e.runStage(context.Background(), stageRun{sessionID: 1, turn: 1},
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

	_, attempts, err := e.runStage(context.Background(), stageRun{sessionID: 1, turn: 1},
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

	_, attempts, err := e.runStage(context.Background(), stageRun{sessionID: 1, turn: 1},
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

	res, attempts, err := e.runStage(context.Background(), stageRun{sessionID: 1, turn: 1},
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

	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"}, nil)
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}
	if len(m.calls) != 4 {
		t.Fatalf("calls = %d, want 4 (GM, NPC A, NPC B, Support)", len(m.calls))
	}
	if !strings.Contains(lastContent(m.calls[1].msgs), "「A」") || !strings.Contains(lastContent(m.calls[2].msgs), "「B」") {
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

	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"}, nil)
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

	res, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"}, nil)
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

	if _, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: session.ID, Turn: 1, Text: "魔法で攻撃"}, nil); err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}
	// Each stage's rules are in its own instruction, the last message.
	gmPrompt := lastContent(m.calls[0].msgs)
	supportPrompt := lastContent(m.calls[2].msgs)
	for name, prompt := range map[string]string{"GM": gmPrompt, "Support": supportPrompt} {
		if !strings.Contains(prompt, "[第三部 データ > 魔法データ > 真語魔法データ p.248-249] 【エネルギー・ボルト】消費MP5") {
			t.Errorf("%s prompt lacks the rule: %q", name, prompt)
		}
		if strings.Count(prompt, "【参照ルール】") != 1 {
			t.Errorf("%s prompt should have exactly one rules header: %q", name, prompt)
		}
	}
}

func lastContent(msgs []llm.Message) string { return msgs[len(msgs)-1].Content }

// Every stage of a turn must extend the previous stage's request token for
// token, so mlx-vlm's prefix cache only has to prefill the new instruction.
func TestProcessTurnStagesShareOnePrefix(t *testing.T) {
	bad := llm.Result{Text: "name: B\naction: 攻撃\ndialogue: x\nmp_cost: 99", FinishReason: "stop"}
	e, m, sid := setupSerialEngine(t, []llm.Result{
		{Text: "GMの描写。", FinishReason: "stop"},
		{Text: npcYAML("A"), FinishReason: "stop"},
		bad, bad, bad,
		{Text: npcYAML("C"), FinishReason: "stop"},
		{Text: `[{"npc_name":"A","hp_delta":-5,"mp_cost":3},{"npc_name":"C","hp_delta":-5,"mp_cost":3}]`, FinishReason: "stop"},
	}, "A", "B", "C")

	if _, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: 1, Text: "進む"}, nil); err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}
	// First attempts only: GM, NPC A, NPC B, NPC C, Support.
	first := []scriptedCall{m.calls[0], m.calls[1], m.calls[2], m.calls[5], m.calls[6]}
	for i := 1; i < len(first); i++ {
		prev, cur := first[i-1].msgs, first[i].msgs
		// The previous request minus its own instruction is a prefix of this one.
		if !hasPrefix(cur, prev[:len(prev)-1]) {
			t.Fatalf("call %d does not extend call %d:\nprev=%+v\ncur=%+v", i, i-1, prev, cur)
		}
	}
	if first[0].msgs[0].Role != "system" {
		t.Error("the shared system prompt should come first")
	}
	// A's accepted answer is visible to C and Support; B's rejected ones are not.
	for _, c := range first[3:] {
		var joined strings.Builder
		for _, msg := range c.msgs {
			joined.WriteString(msg.Content)
		}
		if !strings.Contains(joined.String(), npcYAML("A")) {
			t.Error("later stages should see NPC A's action")
		}
		if strings.Contains(joined.String(), "mp_cost: 99") {
			t.Error("a rejected NPC answer must not enter the shared transcript")
		}
	}
	// The retry of B works on its own copy and leaves the transcript intact.
	if got := m.calls[3].msgs; len(got) != len(m.calls[2].msgs)+2 {
		t.Errorf("retry of B has %d messages, want %d", len(got), len(m.calls[2].msgs)+2)
	}
}

func TestStageSystemPromptIsStable(t *testing.T) {
	e, m, sid := setupSerialEngine(t, []llm.Result{
		{Text: "GMの描写1。", FinishReason: "stop"},
		{Text: npcYAML("A"), FinishReason: "stop"},
		{Text: `[{"npc_name":"A","hp_delta":-5,"mp_cost":3}]`, FinishReason: "stop"},
		{Text: "GMの描写2。", FinishReason: "stop"},
		{Text: npcYAML("A"), FinishReason: "stop"},
		{Text: `[{"npc_name":"A","hp_delta":0,"mp_cost":0}]`, FinishReason: "stop"},
	}, "A")
	for turn := 1; turn <= 2; turn++ {
		if _, err := e.ProcessTurn(context.Background(), PlayerInput{SessionID: sid, Turn: turn, Text: "進む"}, nil); err != nil {
			t.Fatalf("turn %d: %v", turn, err)
		}
	}
	// HP / MP changed between turns, but the system prompt must not.
	if m.calls[0].msgs[0] != m.calls[3].msgs[0] {
		t.Errorf("system prompt changed between turns:\n%q\n%q", m.calls[0].msgs[0].Content, m.calls[3].msgs[0].Content)
	}
}

func hasPrefix(msgs, prefix []llm.Message) bool {
	if len(prefix) > len(msgs) {
		return false
	}
	for i := range prefix {
		if msgs[i] != prefix[i] {
			return false
		}
	}
	return true
}
