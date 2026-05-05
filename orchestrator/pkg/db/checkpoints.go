package db

import (
	"database/sql"
	"fmt"
	"time"
)

const maxCheckpoints = 10

type Checkpoint struct {
	ID           int64
	SessionID    int64
	Turn         int
	SnapshotJSON string
	CreatedAt    time.Time
}

func SaveCheckpoint(d *sql.DB, sessionID int64, turn int, snapshotJSON string) error {
	_, err := d.Exec(
		`INSERT INTO checkpoints (session_id, turn, snapshot_json) VALUES (?, ?, ?)`,
		sessionID, turn, snapshotJSON,
	)
	if err != nil {
		return fmt.Errorf("SaveCheckpoint: %w", err)
	}
	return pruneCheckpoints(d, sessionID)
}

func pruneCheckpoints(d *sql.DB, sessionID int64) error {
	_, err := d.Exec(`
		DELETE FROM checkpoints
		WHERE session_id = ? AND id NOT IN (
			SELECT id FROM checkpoints WHERE session_id = ?
			ORDER BY created_at DESC LIMIT ?
		)`, sessionID, sessionID, maxCheckpoints)
	return err
}

func GetCheckpoint(d *sql.DB, sessionID int64, turn int) (*Checkpoint, error) {
	row := d.QueryRow(
		`SELECT id, session_id, turn, snapshot_json, created_at
		 FROM checkpoints WHERE session_id = ? AND turn = ?`,
		sessionID, turn,
	)
	cp := &Checkpoint{}
	if err := row.Scan(&cp.ID, &cp.SessionID, &cp.Turn, &cp.SnapshotJSON, &cp.CreatedAt); err != nil {
		return nil, fmt.Errorf("GetCheckpoint(session=%d turn=%d): %w", sessionID, turn, err)
	}
	return cp, nil
}

func ListCheckpoints(d *sql.DB, sessionID int64) ([]Checkpoint, error) {
	rows, err := d.Query(
		`SELECT id, session_id, turn, snapshot_json, created_at
		 FROM checkpoints WHERE session_id = ? ORDER BY turn DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		var cp Checkpoint
		if err := rows.Scan(&cp.ID, &cp.SessionID, &cp.Turn, &cp.SnapshotJSON, &cp.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, rows.Err()
}
