package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
)

func setupTest(t *testing.T) (*Engine, *sql.DB) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Mock embed server: returns a fixed vector based on content length
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Input string }
		json.NewDecoder(r.Body).Decode(&req)
		// Simple deterministic vector: [1,0,0] for "rulebook", [0,1,0] for others
		vec := []float32{1, 0, 0}
		if req.Input != "ルール本文テスト" {
			vec = []float32{0, 1, 0}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vec}},
		})
	}))
	t.Cleanup(srv.Close)

	client := llm.New(srv.URL, "test-embed", 5*time.Second)
	engine := New(database, client)
	return engine, database
}

func TestIndexAndSearch(t *testing.T) {
	engine, _ := setupTest(t)
	ctx := context.Background()

	// Index two chunks with different embeddings
	_, err := engine.IndexChunk(ctx, "rulebook", 0, "ルール本文テスト")
	if err != nil {
		t.Fatalf("IndexChunk rulebook: %v", err)
	}
	_, err = engine.IndexChunk(ctx, "houserule", 20, "ハウスルール")
	if err != nil {
		t.Fatalf("IndexChunk houserule: %v", err)
	}

	// Query that embeds to [1,0,0] — should match "ルール本文テスト"
	results, err := engine.Search(ctx, "ルール本文テスト", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SourceType != "rulebook" {
		t.Errorf("expected rulebook, got %s", results[0].SourceType)
	}
}

func TestBuildContext(t *testing.T) {
	engine, _ := setupTest(t)
	chunks := []db.RAGChunk{
		{SourceType: "houserule", Text: "ハウスルールA"},
		{SourceType: "correction", Text: "訂正C"},
		{SourceType: "rulebook", Section: "第二部 ルール > 戦闘のルール", PageFrom: 128, PageTo: 129, Text: "ルールB"},
	}
	got := engine.BuildContext(chunks)
	want := "【参照ルール】\n" +
		"[ハウスルール] ハウスルールA\n" +
		"[訂正] 訂正C\n" +
		"[第二部 ルール > 戦闘のルール p.128-129] ルールB\n"
	if got != want {
		t.Errorf("BuildContext =\n%s\nwant\n%s", got, want)
	}
}

func TestEmbedInputPrefixesSection(t *testing.T) {
	if got := EmbedInput("第二部 > 戦闘", "本文"); got != "第二部 > 戦闘\n本文" {
		t.Errorf("got %q", got)
	}
	if got := EmbedInput("", "本文"); got != "本文" {
		t.Errorf("got %q", got)
	}
}

func TestReembedUpdatesEmbeddingAfterEdit(t *testing.T) {
	engine, database := setupTest(t)
	ctx := context.Background()

	id, err := engine.IndexChunk(ctx, "houserule", 20, "ハウスルール")
	if err != nil {
		t.Fatal(err)
	}
	// Edit the text so it now embeds to [1,0,0] (the mock's vector for "ルール本文テスト")
	if err := db.UpdateRAGChunk(database, id, "ルール本文テスト", "", true); err != nil {
		t.Fatal(err)
	}
	if err := engine.Reembed(ctx, id); err != nil {
		t.Fatalf("Reembed: %v", err)
	}
	results, err := engine.Search(ctx, "ルール本文テスト", 1)
	if err != nil || len(results) != 1 || results[0].ID != id {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestBuildContextEmpty(t *testing.T) {
	engine, _ := setupTest(t)
	if got := engine.BuildContext(nil); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestSearchPutsTermDefinitionFirst(t *testing.T) {
	engine, database := setupTest(t)
	ctx := context.Background()
	// The mock embeds every query to the same vector as "ルール本文テスト" only
	// when the text matches; anything else embeds elsewhere, so similarity alone
	// would never find the feat for this query.
	_, _ = engine.IndexChunk(ctx, "rulebook", 0, "ルール本文テスト")
	featID, _ := db.InsertRAGChunk(database, db.RAGChunk{
		SourceType: "rulebook", Enabled: true, Text: "味方への攻撃を代わりに受ける。",
		Section: "第三部 データ > 戦闘特技データ > 《かばうⅠ》", Embedding: []float32{0, 0, 1},
	})
	got, err := engine.Search(ctx, "盾で割り込んで後衛を庇う", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != featID || got[0].MatchedTerm != "かばう" {
		t.Errorf("got %+v", got)
	}
}
