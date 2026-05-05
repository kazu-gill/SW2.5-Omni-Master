package db

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

type RAGChunk struct {
	ID          int64
	SourceType  string
	Priority    int
	Tag         string
	Text        string
	Enabled     bool
	OverridesID *int64
	Embedding   []float32
}

func InsertRAGChunk(d *sql.DB, chunk RAGChunk) (int64, error) {
	var embBlob []byte
	if len(chunk.Embedding) > 0 {
		embBlob = float32SliceToBlob(chunk.Embedding)
	}
	enabled := 1
	if !chunk.Enabled {
		enabled = 0
	}
	res, err := d.Exec(
		`INSERT INTO rag_chunks (source_type, priority, tag, text, enabled, overrides_id, embedding)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		chunk.SourceType, chunk.Priority, chunk.Tag, chunk.Text, enabled, chunk.OverridesID, embBlob,
	)
	if err != nil {
		return 0, fmt.Errorf("InsertRAGChunk: %w", err)
	}
	return res.LastInsertId()
}

func ListRAGChunks(d *sql.DB, sourceType string) ([]RAGChunk, error) {
	q := `SELECT id, source_type, priority, tag, text, enabled, overrides_id
	      FROM rag_chunks`
	args := []any{}
	if sourceType != "" {
		q += ` WHERE source_type = ?`
		args = append(args, sourceType)
	}
	q += ` ORDER BY priority DESC, id ASC`

	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RAGChunk
	for rows.Next() {
		var c RAGChunk
		var enabled int
		var overridesID sql.NullInt64
		if err := rows.Scan(&c.ID, &c.SourceType, &c.Priority, &c.Tag, &c.Text, &enabled, &overridesID); err != nil {
			return nil, err
		}
		c.Enabled = enabled != 0
		if overridesID.Valid {
			v := overridesID.Int64
			c.OverridesID = &v
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func UpdateRAGChunk(d *sql.DB, id int64, text string, tag string, enabled bool) error {
	e := 1
	if !enabled {
		e = 0
	}
	_, err := d.Exec(
		`UPDATE rag_chunks SET text = ?, tag = ?, enabled = ? WHERE id = ?`,
		text, tag, e, id,
	)
	return err
}

func DeleteRAGChunk(d *sql.DB, id int64) error {
	_, err := d.Exec(`DELETE FROM rag_chunks WHERE id = ?`, id)
	return err
}

// ── rag_chunks_opt ──────────────────────────────────────────────────────────

type RAGChunkOpt struct {
	ID          int64
	ChunkID     int64
	Text        string
	Tag         string
	Summary     string
	Embedding   []float32
	Model       string
	OptimizedAt string
}

// UpsertRAGChunkOpt inserts or replaces an optimized chunk record and marks
// the source chunk's opt_status as 'done'.
func UpsertRAGChunkOpt(d *sql.DB, opt RAGChunkOpt) error {
	var embBlob []byte
	if len(opt.Embedding) > 0 {
		embBlob = float32SliceToBlob(opt.Embedding)
	}
	_, err := d.Exec(`
		INSERT INTO rag_chunks_opt (chunk_id, text, tag, summary, embedding, model)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(chunk_id) DO UPDATE SET
		  text=excluded.text, tag=excluded.tag, summary=excluded.summary,
		  embedding=excluded.embedding, model=excluded.model,
		  optimized_at=CURRENT_TIMESTAMP`,
		opt.ChunkID, opt.Text, opt.Tag, opt.Summary, embBlob, opt.Model,
	)
	if err != nil {
		return fmt.Errorf("UpsertRAGChunkOpt: %w", err)
	}
	_, err = d.Exec(`UPDATE rag_chunks SET opt_status='done' WHERE id=?`, opt.ChunkID)
	return err
}

// MarkOptStatus sets opt_status on a rag_chunks row (e.g. 'failed'/'skip').
func MarkOptStatus(d *sql.DB, chunkID int64, status string) error {
	_, err := d.Exec(`UPDATE rag_chunks SET opt_status=? WHERE id=?`, status, chunkID)
	return err
}

// OptStats returns counts by opt_status for monitoring.
func OptStats(d *sql.DB) (map[string]int, error) {
	rows, err := d.Query(`
		SELECT COALESCE(opt_status,'unprocessed'), count(*)
		FROM rag_chunks WHERE enabled=1 GROUP BY opt_status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var v int
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func UpdateEmbedding(d *sql.DB, chunkID int64, embedding []float32) error {
	_, err := d.Exec(
		`UPDATE rag_chunks SET embedding = ? WHERE id = ?`,
		float32SliceToBlob(embedding), chunkID,
	)
	return err
}

// SearchRAGChunks finds the topK most similar chunks to queryVec using cosine
// similarity. Only enabled chunks are considered. Priority (20 > 10 > 0) is
// used as a tiebreaker at equal similarity.
// When a rag_chunks_opt record exists for a chunk, its text and embedding are
// used instead of the originals.
func SearchRAGChunks(d *sql.DB, queryVec []float32, topK int) ([]RAGChunk, error) {
	rows, err := d.Query(`
		SELECT r.id, r.source_type, r.priority,
		       COALESCE(o.tag, r.tag)           AS tag,
		       COALESCE(o.text, r.text)          AS text,
		       COALESCE(o.embedding, r.embedding) AS embedding
		FROM rag_chunks r
		LEFT JOIN rag_chunks_opt o ON o.chunk_id = r.id
		WHERE r.enabled = 1
		  AND COALESCE(o.embedding, r.embedding) IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		chunk RAGChunk
		score float64
	}
	var candidates []scored

	for rows.Next() {
		var c RAGChunk
		var blob []byte
		if err := rows.Scan(&c.ID, &c.SourceType, &c.Priority, &c.Tag, &c.Text, &blob); err != nil {
			return nil, err
		}
		c.Enabled = true
		c.Embedding = blobToFloat32Slice(blob)
		sim := cosineSimilarity(queryVec, c.Embedding)
		candidates = append(candidates, scored{c, sim})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].chunk.Priority > candidates[j].chunk.Priority
	})

	if topK > len(candidates) {
		topK = len(candidates)
	}
	out := make([]RAGChunk, topK)
	for i := range out {
		out[i] = candidates[i].chunk
	}
	return out, nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func float32SliceToBlob(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func blobToFloat32Slice(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}
