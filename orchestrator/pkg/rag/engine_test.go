package rag

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
		var req struct{ Content string }
		json.NewDecoder(r.Body).Decode(&req)
		// Simple deterministic vector: [1,0,0] for "rulebook", [0,1,0] for others
		vec := []float32{1, 0, 0}
		if req.Content != "ルール本文テスト" {
			vec = []float32{0, 1, 0}
		}
		json.NewEncoder(w).Encode(map[string]any{"embedding": vec})
	}))
	t.Cleanup(srv.Close)

	client := llm.New(srv.URL)
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
		{SourceType: "rulebook", Text: "ルールB"},
	}
	ctx := engine.BuildContext(chunks)
	if ctx == "" {
		t.Fatal("BuildContext returned empty string")
	}
	if ctx[:len("【参照ルール】")] != "【参照ルール】" {
		t.Errorf("BuildContext missing header: %q", ctx)
	}
}

func TestBuildContextEmpty(t *testing.T) {
	engine, _ := setupTest(t)
	if got := engine.BuildContext(nil); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
