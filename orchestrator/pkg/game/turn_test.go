package game

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
)

func mockLLMServer(reply string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/embedding":
			json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{1, 0, 0}})
		default:
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]string{"role": "assistant", "content": reply}},
				},
			})
		}
	}))
}

func setupEngine(t *testing.T, npcReply string) (*Engine, int64) {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	gmSrv := mockLLMServer("GMが状況を描写します。")
	npcSrv := mockLLMServer(npcReply)
	supportSrv := mockLLMServer(`[{"npc_name":"ガルド","hp_delta":-5,"mp_cost":3}]`)
	embedSrv := mockLLMServer("")
	t.Cleanup(func() { gmSrv.Close(); npcSrv.Close(); supportSrv.Close(); embedSrv.Close() })

	pool := LLMPool{
		GM:      llm.New(gmSrv.URL),
		NPCs:    [3]*llm.Client{llm.New(npcSrv.URL), llm.New(npcSrv.URL), llm.New(npcSrv.URL)},
		Support: llm.New(supportSrv.URL),
	}
	ragEngine := rag.New(database, llm.New(embedSrv.URL))
	engine := NewEngine(database, pool, ragEngine)

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
		name      string
		action    NPCAction
		hp, mp    int
		wantErr   bool
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
