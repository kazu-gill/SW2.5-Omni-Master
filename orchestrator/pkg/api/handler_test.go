package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/game"
	"github.com/kf/sw25-omni-master/pkg/llm"
	"github.com/kf/sw25-omni-master/pkg/rag"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// mockLLMSrv answers every turn stage from one endpoint, choosing the reply
// by the stage's instruction, which is always the last message.
func mockLLMSrv(gm, npc, support string) *httptest.Server {
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
		switch instruction := req.Messages[len(req.Messages)-1].Content; {
		case strings.Contains(instruction, "あなたはNPC"):
			reply = npc
		case strings.Contains(instruction, "数値計算担当"):
			reply = support
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": reply}, "finish_reason": "stop"},
			},
		})
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
	llmSrv := mockLLMSrv("GMが状況を描写します。", npcYAML, `[{"npc_name":"ガルド","hp_delta":0,"mp_cost":0}]`)
	t.Cleanup(llmSrv.Close)

	chat := llm.New(llmSrv.URL, "test-model", 5*time.Second)
	ragEngine := rag.New(database, llm.New(llmSrv.URL, "test-embed", 5*time.Second))
	engine := game.NewEngine(database, chat, ragEngine, 2)

	session, _ := db.CreateSession(database, "test")
	_ = db.UpsertNPCSheet(database, db.NPCSheet{SessionID: session.ID, Name: "ガルド", HP: 30, MP: 10})

	h := New(database, engine, ragEngine, nil, "")
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

func TestCreateAndEditRuleAreEmbedded(t *testing.T) {
	h, srv, _ := setupHandler(t)

	body := `{"source_type":"houserule","tag":"combat","text":"クリティカル時は追加で1d振る"}`
	resp, err := http.Post(srv.URL+"/api/rules", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID       int64 `json:"id"`
		Embedded bool  `json:"embedded"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || !created.Embedded {
		t.Fatalf("status=%d created=%+v", resp.StatusCode, created)
	}
	var n int
	h.database.QueryRow(`SELECT count(*) FROM rag_chunks WHERE id = ? AND embedding IS NOT NULL`, created.ID).Scan(&n)
	if n != 1 {
		t.Fatal("new rule should have an embedding so RAG can find it")
	}

	// Editing re-embeds: clear the embedding, PATCH, and expect it back
	h.database.Exec(`UPDATE rag_chunks SET embedding = NULL WHERE id = ?`, created.ID)
	req, _ := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/rules/%d", srv.URL, created.ID),
		strings.NewReader(`{"text":"クリティカル時は追加で2d振る","tag":"combat","enabled":true}`))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	h.database.QueryRow(`SELECT count(*) FROM rag_chunks WHERE id = ? AND embedding IS NOT NULL`, created.ID).Scan(&n)
	if resp.StatusCode != http.StatusNoContent || n != 1 {
		t.Fatalf("status=%d embedded=%d", resp.StatusCode, n)
	}
}

func TestListRulesIncludesSectionAndPages(t *testing.T) {
	h, srv, _ := setupHandler(t)
	_, _ = db.InsertRAGChunk(h.database, db.RAGChunk{SourceType: "rulebook", Section: "第二部 ルール > 戦闘のルール",
		PageFrom: 126, PageTo: 127, Text: "戦闘の基本", Enabled: true})

	resp, err := http.Get(srv.URL + "/api/rules?source_type=rulebook")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rules []map[string]any
	json.NewDecoder(resp.Body).Decode(&rules)
	if len(rules) != 1 || rules[0]["section"] != "第二部 ルール > 戦闘のルール" ||
		rules[0]["page_from"] != float64(126) || rules[0]["page_to"] != float64(127) {
		t.Errorf("rules = %v", rules)
	}
}
