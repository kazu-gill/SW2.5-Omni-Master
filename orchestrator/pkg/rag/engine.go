package rag

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/kf/sw25-omni-master/pkg/db"
	"github.com/kf/sw25-omni-master/pkg/llm"
)

// Engine wraps the DB and LLM client for RAG operations.
type Engine struct {
	database *sql.DB
	embedder *llm.Client

	termsMu sync.Mutex
	terms   *termIndex // built on first search; nil after rules change
}

func New(database *sql.DB, embedder *llm.Client) *Engine {
	return &Engine{database: database, embedder: embedder}
}

// maxTermHits caps how many results the term index may take over.
const maxTermHits = 2

// Search returns the topK rules for query: chunks that define a game term the
// query names (by reading, see terms.go) come first, then the most similar
// chunks by embedding (with the priority bonus).
func (e *Engine) Search(ctx context.Context, query string, topK int) ([]db.RAGChunk, error) {
	vec, err := e.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("rag.Search embed: %w", err)
	}
	found, err := db.SearchRAGChunks(e.database, vec, topK)
	if err != nil {
		return nil, err
	}
	matches := e.termIndex().match(query, min(maxTermHits, topK))
	if len(matches) == 0 {
		return found, nil
	}
	var out []db.RAGChunk
	seen := map[int64]bool{}
	for _, m := range matches {
		if seen[m.chunkID] {
			continue
		}
		c, err := db.GetRAGChunk(e.database, m.chunkID)
		if err != nil || !c.Enabled {
			continue
		}
		for _, f := range found {
			if f.ID == c.ID {
				c.Score = f.Score
			}
		}
		c.MatchedTerm = m.name
		seen[c.ID] = true
		out = append(out, c)
	}
	for _, f := range found {
		if len(out) == topK {
			break
		}
		if !seen[f.ID] {
			seen[f.ID] = true
			out = append(out, f)
		}
	}
	return out, nil
}

// termIndex returns the term index, building it from the enabled rules the
// first time (about 0.3 s for the rulebook).
func (e *Engine) termIndex() *termIndex {
	e.termsMu.Lock()
	defer e.termsMu.Unlock()
	if e.terms == nil {
		chunks, err := db.ListRAGChunks(e.database, "")
		if err != nil {
			return &termIndex{}
		}
		e.terms = buildTermIndex(chunks)
	}
	return e.terms
}

// InvalidateTerms makes the next search rebuild the term index (after a rule
// is added, edited or deleted).
func (e *Engine) InvalidateTerms() {
	e.termsMu.Lock()
	e.terms = nil
	e.termsMu.Unlock()
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
	e.InvalidateTerms()
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
