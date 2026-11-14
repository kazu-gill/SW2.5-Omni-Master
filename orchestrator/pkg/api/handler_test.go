package api

import (
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
	"github.com/kf/sw25-omni-master/pkg/llm/llmtest"
	"github.com/kf/sw25-omni-master/pkg/rag"
)

// mockLLMSrv answers every turn stage from one endpoint, choosing the reply
// by the stage's instruction, which is always the last message.
func mockLLMSrv(gm, npc, support string) *httptest.Server {
	return llmtest.NewServer(llmtest.ByStage(gm, npc, support))
}

// mockStageLLM is the fake LLM of setupHandler: a GM line, ガルド waiting,
// and no HP/MP change.
func mockStageLLM() *httptest.Server {
	npcYAML := "name: ガルド\naction: 待機\ndialogue: 様子を見る\nmp_cost: 0\nhp_delta: 0"
	return mockLLMSrv("GMが状況を描写します。", npcYAML, `[{"npc_name":"ガルド","hp_delta":0,"mp_cost":0}]`)
}

func setupHandler(t *testing.T) (*Handler, *httptest.Server, int64) {
	t.Helper()
	llmSrv := mockStageLLM()
	t.Cleanup(llmSrv.Close)
	return setupHandlerLLM(t, llmSrv.URL)
}

// setupHandlerLLM is setupHandler with the LLM at llmURL.
func setupHandlerLLM(t *testing.T, llmURL string) (*Handler, *httptest.Server, int64) {
	t.Helper()

	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	chat := llm.New(llmURL, "test-model", 5*time.Second)
	ragEngine := rag.New(database, llm.New(llmURL, "test-embed", 5*time.Second))
	engine := game.NewEngine(database, chat, ragEngine, 2)

	session, _ := db.CreateSession(database, "test")
	_ = db.UpsertNPCSheet(database, db.NPCSheet{SessionID: session.ID, Name: "ガルド", HP: 30, MP: 10})

	h := New(database, engine, ragEngine, Options{TurnTimeout: 30 * time.Second, PersonasDir: "../../../data/personas"})
	srv := httptest.NewServer(func() http.Handler {
		mux := http.NewServeMux()
		h.Routes(mux)
		return mux
	}())
	t.Cleanup(srv.Close)

	return h, srv, session.ID
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
	var page struct {
		Total int              `json:"total"`
		Items []map[string]any `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&page)
	rules := page.Items
	if len(rules) != 1 || rules[0]["section"] != "第二部 ルール > 戦闘のルール" ||
		rules[0]["page_from"] != float64(126) || rules[0]["page_to"] != float64(127) {
		t.Errorf("rules = %v", rules)
	}
}
