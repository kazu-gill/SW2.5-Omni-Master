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
	database *sql.DB
	embedder *llm.Client
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

// EmbedInput is the text that gets embedded for a chunk: its section path
// (部 > 章 > 見出し) followed by the body, so a query about a topic matches
// chunks whose body never names it. ai-agents/embed_chunks.py builds the same.
func EmbedInput(section, text string) string {
	if section == "" {
		return text
	}
	return section + "\n" + text
}

var sourceLabels = map[string]string{"houserule": "ハウスルール", "correction": "訂正"}

// BuildContext formats RAG chunks into a single context string for a prompt.
// Each chunk is labeled with where it comes from: a houserule / correction,
// or the rulebook section and page.
func (e *Engine) BuildContext(chunks []db.RAGChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【参照ルール】\n")
	for _, c := range chunks {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", chunkLabel(c), c.Text))
	}
	return sb.String()
}

func chunkLabel(c db.RAGChunk) string {
	if l, ok := sourceLabels[c.SourceType]; ok {
		return l
	}
	label := c.Section
	if label == "" {
		label = c.SourceType
	}
	switch {
	case c.PageFrom > 0 && c.PageTo > c.PageFrom:
		label += fmt.Sprintf(" p.%d-%d", c.PageFrom, c.PageTo)
	case c.PageFrom > 0:
		label += fmt.Sprintf(" p.%d", c.PageFrom)
	}
	return label
}

// Reembed regenerates a chunk's embedding from its current section and text,
// e.g. after the text was edited.
func (e *Engine) Reembed(ctx context.Context, id int64) error {
	c, err := db.GetRAGChunk(e.database, id)
	if err != nil {
		return err
	}
	vec, err := e.embedder.Embed(ctx, EmbedInput(c.Section, c.Text))
	if err != nil {
		return fmt.Errorf("rag.Reembed: %w", err)
	}
	return db.UpdateEmbedding(e.database, id, vec)
}

// IndexChunk stores a text chunk and immediately generates its embedding.
func (e *Engine) IndexChunk(ctx context.Context, sourceType string, priority int, text string) (int64, error) {
	vec, err := e.embedder.Embed(ctx, EmbedInput("", text))
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
