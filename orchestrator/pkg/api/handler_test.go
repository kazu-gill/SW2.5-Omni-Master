package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/game"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func mockLLMSrv(reply string) *httptest.Server {
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

func setupHandler(t *testing.T) (*Handler, *httptest.Server, int64) {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	npcYAML := "name: ガルド\naction: 待機\ndialogue: 様子を見る\nmp_cost: 0\nhp_delta: 0"
	gmSrv := mockLLMSrv("GMが状況を描写します。")
	npcSrv := mockLLMSrv(npcYAML)
	supportSrv := mockLLMSrv(`[{"npc_name":"ガルド","hp_delta":0,"mp_cost":0}]`)
	t.Cleanup(func() { gmSrv.Close(); npcSrv.Close(); supportSrv.Close() })

	pool := game.LLMPool{
		GM:      llm.New(gmSrv.URL),
		NPCs:    [3]*llm.Client{llm.New(npcSrv.URL), llm.New(npcSrv.URL), llm.New(npcSrv.URL)},
		Support: llm.New(supportSrv.URL),
	}
	ragEngine := rag.New(database, llm.New(gmSrv.URL))
	engine := game.NewEngine(database, pool, ragEngine)

	session, _ := db.CreateSession(database, "test")
	_ = db.UpsertNPCSheet(database, db.NPCSheet{SessionID: session.ID, Name: "ガルド", HP: 30, MP: 10})

	h := New(database, engine, nil, "")
	srv := httptest.NewServer(func() http.Handler {
		mux := http.NewServeMux()
		h.Routes(mux)
		return mux
	}())
	t.Cleanup(srv.Close)

	return h, srv, session.ID
}

func TestHandleTurn(t *testing.T) {
	_, srv, sessionID := setupHandler(t)

	body, _ := json.Marshal(game.PlayerInput{SessionID: sessionID, Turn: 1, Text: "攻撃する"})
	resp, err := http.Post(srv.URL+"/api/turn", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/turn: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result game.TurnResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.GMNarration == "" {
		t.Error("GMNarration should not be empty")
	}
}

func TestHandleGetSession(t *testing.T) {
	_, srv, sessionID := setupHandler(t)

	resp, err := http.Get(srv.URL + "/api/session/1")
	if err != nil {
		t.Fatalf("GET /api/session/1: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	_ = sessionID
}

func TestHandleTurnMissingFields(t *testing.T) {
	_, srv, _ := setupHandler(t)

	body := bytes.NewBufferString(`{"text":"hello"}`) // missing session_id
	resp, err := http.Post(srv.URL+"/api/turn", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/turn: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestWebSocketBroadcast(t *testing.T) {
	_, srv, sessionID := setupHandler(t)

	// Connect a WebSocket client
	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("WS dial: %v", err)
	}
	defer conn.CloseNow()

	// Trigger a turn — this should broadcast to the WS client
	body, _ := json.Marshal(game.PlayerInput{SessionID: sessionID, Turn: 1, Text: "行動する"})
	go http.Post(srv.URL+"/api/turn", "application/json", bytes.NewReader(body))

	// Expect a message within 5 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var msg game.TurnResult
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("WS read: %v", err)
	}
	if msg.GMNarration == "" {
		t.Error("broadcast should contain GMNarration")
	}
}
