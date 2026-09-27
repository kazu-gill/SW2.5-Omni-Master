package game

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
)

// mockLLMServer answers every stage from one endpoint, choosing the reply by
// the stage's system prompt (all stages share one model in production).
func mockLLMServer(gm, npc, support string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": []float32{1, 0, 0}}},
			})
			return
		}
		var req struct {
			Messages []llm.Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		reply := gm
		switch system := req.Messages[0].Content; {
		case strings.Contains(system, "あなたはNPC"):
			reply = npc
		case strings.Contains(system, "数値計算担当"):
			reply = support
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": reply}, "finish_reason": "stop"},
			},
		})
	}))
}

func setupEngine(t *testing.T, npcReply string) (*Engine, int64) {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	srv := mockLLMServer("GMが状況を描写します。", npcReply, `[{"npc_name":"ガルド","hp_delta":-5,"mp_cost":3}]`)
	t.Cleanup(srv.Close)

	chat := llm.New(srv.URL, "test-model", 5*time.Second)
	ragEngine := rag.New(database, llm.New(srv.URL, "test-embed", 5*time.Second))
	engine := NewEngine(database, chat, ragEngine, 2)

	// Create session and NPC sheets
	session, _ := db.CreateSession(database, "test-quest")
	_ = db.UpsertNPCSheet(database, db.NPCSheet{
		SessionID: session.ID, Name: "ガルド", HP: 30, MP: 15,
	})

	return engine, session.ID
}

func TestProcessTurnBasic(t *testing.T) {
	npcYAML := "name: ガルド\naction: 攻撃\ndialogue: いくぞ！\nmp_cost: 3\nhp_delta: -5"
	engine, sessionID := setupEngine(t, npcYAML)

	result, err := engine.ProcessTurn(context.Background(), PlayerInput{
		SessionID: sessionID,
		Turn:      1,
		Text:      "剣で攻撃する",
	})
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}
	if result.GMNarration == "" {
		t.Error("GMNarration should not be empty")
	}
	if result.Turn != 1 {
		t.Errorf("Turn = %d, want 1", result.Turn)
	}
}

func TestProcessTurnCheckpointSaved(t *testing.T) {
	npcYAML := "name: ガルド\naction: 待機\ndialogue: 様子を見る\nmp_cost: 0\nhp_delta: 0"
	engine, sessionID := setupEngine(t, npcYAML)

	database := engine.db
	_, err := engine.ProcessTurn(context.Background(), PlayerInput{
		SessionID: sessionID,
		Turn:      1,
		Text:      "様子を見る",
	})
	if err != nil {
		t.Fatalf("ProcessTurn: %v", err)
	}

	cps, err := db.ListCheckpoints(database, sessionID)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) == 0 {
		t.Error("expected at least 1 checkpoint after ProcessTurn")
	}
}

func TestValidateNPCAction(t *testing.T) {
	tests := []struct {
		name    string
		action  NPCAction
		hp, mp  int
		wantErr bool
	}{
		{"valid", NPCAction{Name: "A", Action: "attack", Dialogue: "hi", MPCost: 3}, 30, 10, false},
		{"missing name", NPCAction{Action: "x", Dialogue: "y"}, 10, 10, true},
		{"mp overflow", NPCAction{Name: "A", Action: "x", Dialogue: "y", MPCost: 20}, 10, 5, true},
		{"hp underflow", NPCAction{Name: "A", Action: "x", Dialogue: "y", HPDelta: -50}, 10, 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNPCAction(&tt.action, tt.hp, tt.mp)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

func TestParseNPCAction(t *testing.T) {
	yaml := "name: ガルド\naction: 攻撃\ndialogue: いくぞ！\nmp_cost: 3\nhp_delta: -5"
	action, err := ParseNPCAction(yaml)
	if err != nil {
		t.Fatalf("ParseNPCAction: %v", err)
	}
	if action.Name != "ガルド" {
		t.Errorf("Name = %q, want ガルド", action.Name)
	}
	if action.MPCost != 3 {
		t.Errorf("MPCost = %d, want 3", action.MPCost)
	}
}

func TestValidateGMNarration(t *testing.T) {
	tests := []struct {
		name    string
		res     llm.Result
		wantErr bool
	}{
		{"ok", llm.Result{Text: "扉が軋んで開いた。", FinishReason: "stop"}, false},
		{"empty", llm.Result{Text: "  \n", FinishReason: "stop"}, true},
		{"npc yaml", llm.Result{Text: "name: ガルド\naction: 攻撃\ndialogue: いくぞ", FinishReason: "stop"}, true},
		{"json", llm.Result{Text: `[{"npc_name":"ガルド"}]`, FinishReason: "stop"}, true},
		{"refusal", llm.Result{Text: "申し訳ありませんが、その内容にはお答えできません。", FinishReason: "stop"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGMNarration(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

func TestValidateNPCActionFor(t *testing.T) {
	sheet := db.NPCSheet{Name: "ガルド", HP: 30, MP: 10}
	tests := []struct {
		name    string
		action  NPCAction
		wantErr bool
	}{
		{"ok", NPCAction{Name: "ガルド", Action: "攻撃", Dialogue: "いくぞ", MPCost: 3}, false},
		{"ok lane", NPCAction{Name: "ガルド", Action: "突撃", Dialogue: "前へ", NewLane: "front"}, false},
		{"other npc", NPCAction{Name: "リリィ", Action: "攻撃", Dialogue: "いくぞ"}, true},
		{"bad lane", NPCAction{Name: "ガルド", Action: "攻撃", Dialogue: "いくぞ", NewLane: "middle"}, true},
		{"mp overflow", NPCAction{Name: "ガルド", Action: "魔法", Dialogue: "喰らえ", MPCost: 11}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNPCActionFor(&tt.action, sheet)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

func TestParseResourceDeltas(t *testing.T) {
	actions := []NPCAction{{Name: "A"}, {Name: "B"}}
	sheets := []db.NPCSheet{{Name: "A", HP: 10, MP: 5}, {Name: "B", HP: 10, MP: 5}}
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"ok", `[{"npc_name":"A","hp_delta":-3,"mp_cost":2},{"npc_name":"B","hp_delta":0,"mp_cost":0}]`, false},
		{"code fence", "```json\n[{\"npc_name\":\"A\",\"hp_delta\":0,\"mp_cost\":0},{\"npc_name\":\"B\",\"hp_delta\":0,\"mp_cost\":0}]\n```", false},
		{"not json", "計算しました", true},
		{"missing B", `[{"npc_name":"A","hp_delta":0,"mp_cost":0}]`, true},
		{"unknown name", `[{"npc_name":"A","hp_delta":0,"mp_cost":0},{"npc_name":"Z","hp_delta":0,"mp_cost":0}]`, true},
		{"duplicate", `[{"npc_name":"A","hp_delta":0,"mp_cost":0},{"npc_name":"A","hp_delta":0,"mp_cost":0},{"npc_name":"B","hp_delta":0,"mp_cost":0}]`, true},
		{"mp out of range", `[{"npc_name":"A","hp_delta":0,"mp_cost":6},{"npc_name":"B","hp_delta":0,"mp_cost":0}]`, true},
		{"hp out of range", `[{"npc_name":"A","hp_delta":-11,"mp_cost":0},{"npc_name":"B","hp_delta":0,"mp_cost":0}]`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResourceDeltas(tt.raw, actions, sheets)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}
