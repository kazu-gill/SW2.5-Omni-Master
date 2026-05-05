package rag

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
)

// Engine wraps the DB and LLM client for RAG operations.
type Engine struct {
	database  *sql.DB
	embedder  *llm.Client
}

func New(database *sql.DB, embedder *llm.Client) *Engine {
	return &Engine{database: database, embedder: embedder}
}

// Search embeds the query via the LLM client, then retrieves the topK most
// relevant RAG chunks ordered by cosine similarity (priority as tiebreaker).
func (e *Engine) Search(ctx context.Context, query string, topK int) ([]db.RAGChunk, error) {
	vec, err := e.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("rag.Search embed: %w", err)
	}
	return db.SearchRAGChunks(e.database, vec, topK)
}

// BuildContext formats RAG chunks into a single context string for the GM prompt.
// Chunks are concatenated in order with their source_type labeled.
func (e *Engine) BuildContext(chunks []db.RAGChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【参照ルール】\n")
	for _, c := range chunks {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", c.SourceType, c.Text))
	}
	return sb.String()
}

// IndexChunk stores a text chunk and immediately generates its embedding.
func (e *Engine) IndexChunk(ctx context.Context, sourceType string, priority int, text string) (int64, error) {
	vec, err := e.embedder.Embed(ctx, text)
	if err != nil {
		return 0, fmt.Errorf("rag.IndexChunk embed: %w", err)
	}
	id, err := db.InsertRAGChunk(e.database, db.RAGChunk{
		SourceType: sourceType,
		Priority:   priority,
		Text:       text,
		Enabled:    true,
		Embedding:  vec,
	})
	if err != nil {
		return 0, fmt.Errorf("rag.IndexChunk insert: %w", err)
	}
	return id, nil
}
