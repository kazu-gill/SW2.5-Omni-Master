package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// 進行中・停止中のターンの状態。止まったターンは最後に終えた段から再開できる。
const (
	ProgressRunning = "running"
	ProgressStopped = "stopped"
)

type TurnProgress struct {
	SessionID int64
	Turn      int
	Input     string
	Mode      string
	StateJSON string
	Status    string // ProgressRunning | ProgressStopped
	Error     string
	UpdatedAt time.Time
}

// SaveTurnProgress upserts the session's progress row and refreshes updated_at.
func SaveTurnProgress(d *sql.DB, p TurnProgress) error {
	_, err := d.Exec(`
		INSERT INTO turn_progress (session_id, turn, input, mode, state_json, status, error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(session_id) DO UPDATE SET
			turn = excluded.turn, input = excluded.input, mode = excluded.mode,
			state_json = excluded.state_json, status = excluded.status,
			error = excluded.error, updated_at = CURRENT_TIMESTAMP`,
		p.SessionID, p.Turn, p.Input, p.Mode, p.StateJSON, p.Status, p.Error,
	)
	if err != nil {
		return fmt.Errorf("SaveTurnProgress(session=%d): %w", p.SessionID, err)
	}
	return nil
}

// GetTurnProgress returns nil, nil when the session has no progress row.
func GetTurnProgress(d *sql.DB, sessionID int64) (*TurnProgress, error) {
	p := &TurnProgress{}
	err := d.QueryRow(`
		SELECT session_id, turn, input, mode, state_json, status, error, updated_at
		FROM turn_progress WHERE session_id = ?`, sessionID,
	).Scan(&p.SessionID, &p.Turn, &p.Input, &p.Mode, &p.StateJSON, &p.Status, &p.Error, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetTurnProgress(session=%d): %w", sessionID, err)
	}
	return p, nil
}

func MarkTurnStopped(d *sql.DB, sessionID int64, errMsg string) error {
	_, err := d.Exec(
		`UPDATE turn_progress SET status = ?, error = ?, updated_at = CURRENT_TIMESTAMP WHERE session_id = ?`,
		ProgressStopped, errMsg, sessionID,
	)
	if err != nil {
		return fmt.Errorf("MarkTurnStopped(session=%d): %w", sessionID, err)
	}
	return nil
}

// StopRunningTurns flips every running row to stopped (startup: the previous
// process died mid-turn) and returns how many rows changed.
func StopRunningTurns(d *sql.DB) (int64, error) {
	res, err := d.Exec(
		`UPDATE turn_progress SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE status = ?`,
		ProgressStopped, ProgressRunning,
	)
	if err != nil {
		return 0, fmt.Errorf("StopRunningTurns: %w", err)
	}
	return res.RowsAffected()
}

// AnyStoppedTurn reports whether any session has a stopped turn.
func AnyStoppedTurn(d *sql.DB) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT EXISTS(SELECT 1 FROM turn_progress WHERE status = ?)`, ProgressStopped).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("AnyStoppedTurn: %w", err)
	}
	return n == 1, nil
}

func DeleteTurnProgress(d *sql.DB, sessionID int64) error {
	_, err := d.Exec(`DELETE FROM turn_progress WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("DeleteTurnProgress(session=%d): %w", sessionID, err)
	}
	return nil
}

// MaxLogID returns the largest session_logs id of the session (0 if none).
func MaxLogID(d *sql.DB, sessionID int64) (int64, error) {
	var id int64
	err := d.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM session_logs WHERE session_id = ?`, sessionID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("MaxLogID(session=%d): %w", sessionID, err)
	}
	return id, nil
}

// DeleteTurnLogsAfter deletes the given turn's log rows with id > afterID.
func DeleteTurnLogsAfter(d *sql.DB, sessionID int64, turn int, afterID int64) error {
	_, err := d.Exec(
		`DELETE FROM session_logs WHERE session_id = ? AND turn = ? AND id > ?`,
		sessionID, turn, afterID,
	)
	if err != nil {
		return fmt.Errorf("DeleteTurnLogsAfter(session=%d turn=%d): %w", sessionID, turn, err)
	}
	return nil
}

// FinishTurn commits a finished turn in one transaction: sheets, the turn
// result, and removal of the progress row.
func FinishTurn(d *sql.DB, sessionID int64, turn int, sheets []NPCSheet, resultJSON string) error {
	tx, err := d.Begin()
	if err != nil {
		return fmt.Errorf("FinishTurn: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	for _, s := range sheets {
		if err := upsertNPCSheet(tx, s); err != nil {
			return fmt.Errorf("FinishTurn: sheet %q: %w", s.Name, err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO turn_results (session_id, turn, result_json) VALUES (?, ?, ?)
		 ON CONFLICT(session_id, turn) DO UPDATE SET result_json = excluded.result_json`,
		sessionID, turn, resultJSON,
	); err != nil {
		return fmt.Errorf("FinishTurn: turn_results: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM turn_progress WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("FinishTurn: progress: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("FinishTurn: %w", err)
	}
	return nil
}
