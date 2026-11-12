package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type Session struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	QuestID   string    `json:"quest_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const sessionColumns = `id, title, quest_id, created_at, updated_at`

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	s := &Session{}
	if err := row.Scan(&s.ID, &s.Title, &s.QuestID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return s, nil
}

func CreateSession(d *sql.DB, questID string) (*Session, error) {
	return CreateSessionTitled(d, "", questID)
}

func CreateSessionTitled(d *sql.DB, title, questID string) (*Session, error) {
	res, err := d.Exec(`INSERT INTO sessions (title, quest_id) VALUES (?, ?)`, title, questID)
	if err != nil {
		return nil, fmt.Errorf("CreateSession: %w", err)
	}
	id, _ := res.LastInsertId()
	return GetSession(d, id)
}

func GetSession(d *sql.DB, id int64) (*Session, error) {
	s, err := scanSession(d.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id))
	if err != nil {
		return nil, fmt.Errorf("GetSession(%d): %w", id, err)
	}
	return s, nil
}

// ListSessions returns all sessions, most recently updated first.
func ListSessions(d *sql.DB) ([]Session, error) {
	rows, err := d.Query(`SELECT ` + sessionColumns + ` FROM sessions ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func DeleteSession(d *sql.DB, id int64) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func SetSessionQuest(d *sql.DB, id int64, questID string) error {
	_, err := d.Exec(`UPDATE sessions SET quest_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, questID, id)
	return err
}

func TouchSession(d *sql.DB, id int64) error {
	_, err := d.Exec(`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

// LatestTurn returns the highest finished turn number of a session (0 when none).
func LatestTurn(d *sql.DB, sessionID int64) (int, error) {
	var n sql.NullInt64
	err := d.QueryRow(`SELECT MAX(turn) FROM turn_results WHERE session_id = ?`, sessionID).Scan(&n)
	return int(n.Int64), err
}

type NPCSheet struct {
	ID        int64     `json:"id"`
	SessionID int64     `json:"session_id"`
	Name      string    `json:"name"`
	HP        int       `json:"hp"`
	MP        int       `json:"mp"`
	HPMax     int       `json:"hp_max"`
	MPMax     int       `json:"mp_max"`
	PositionX int       `json:"position_x"`
	PositionY int       `json:"position_y"`
	YAMLBlob  string    `json:"yaml_blob"`
	UpdatedAt time.Time `json:"updated_at"`
}

func UpsertNPCSheet(d *sql.DB, s NPCSheet) error {
	return upsertNPCSheet(d, s)
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func upsertNPCSheet(d execer, s NPCSheet) error {
	_, err := d.Exec(`
		INSERT INTO npc_sheets (session_id, name, hp, mp, hp_max, mp_max, position_x, position_y, yaml_blob)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, name) DO UPDATE SET
			hp = excluded.hp, mp = excluded.mp,
			hp_max = CASE WHEN excluded.hp_max > 0 THEN excluded.hp_max ELSE npc_sheets.hp_max END,
			mp_max = CASE WHEN excluded.mp_max > 0 THEN excluded.mp_max ELSE npc_sheets.mp_max END,
			position_x = excluded.position_x, position_y = excluded.position_y,
			yaml_blob = excluded.yaml_blob, updated_at = CURRENT_TIMESTAMP`,
		s.SessionID, s.Name, s.HP, s.MP, s.HPMax, s.MPMax, s.PositionX, s.PositionY, s.YAMLBlob,
	)
	return err
}

func GetNPCSheets(d *sql.DB, sessionID int64) ([]NPCSheet, error) {
	rows, err := d.Query(
		`SELECT id, session_id, name, hp, mp, hp_max, mp_max, position_x, position_y, yaml_blob, updated_at
		 FROM npc_sheets WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NPCSheet
	for rows.Next() {
		var s NPCSheet
		if err := rows.Scan(&s.ID, &s.SessionID, &s.Name, &s.HP, &s.MP, &s.HPMax, &s.MPMax,
			&s.PositionX, &s.PositionY, &s.YAMLBlob, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func DeleteNPCSheet(d *sql.DB, sessionID int64, name string) error {
	_, err := d.Exec(`DELETE FROM npc_sheets WHERE session_id = ? AND name = ?`, sessionID, name)
	return err
}

func AppendLog(d *sql.DB, sessionID int64, turn int, role, content string) error {
	_, err := d.Exec(
		`INSERT INTO session_logs (session_id, turn, role, content) VALUES (?, ?, ?, ?)`,
		sessionID, turn, role, content,
	)
	return err
}

func SaveTurnResult(d *sql.DB, sessionID int64, turn int, resultJSON string) error {
	_, err := d.Exec(
		`INSERT INTO turn_results (session_id, turn, result_json) VALUES (?, ?, ?)
		 ON CONFLICT(session_id, turn) DO UPDATE SET result_json = excluded.result_json`,
		sessionID, turn, resultJSON,
	)
	return err
}

func ListTurnResults(d *sql.DB, sessionID int64) ([]string, error) {
	rows, err := d.Query(
		`SELECT result_json FROM turn_results WHERE session_id = ? ORDER BY turn ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func DeleteTurnResultsAfter(d *sql.DB, sessionID int64, turn int) error {
	_, err := d.Exec(
		`DELETE FROM turn_results WHERE session_id = ? AND turn > ?`, sessionID, turn)
	return err
}

func DeleteSessionLogsAfter(d *sql.DB, sessionID int64, turn int) error {
	_, err := d.Exec(
		`DELETE FROM session_logs WHERE session_id = ? AND turn > ?`, sessionID, turn)
	return err
}

// ── 巻き戻し ────────────────────────────────────────────────────────────────

// RestoreBeforeTurn puts a session back to the state saved at the start of
// turn (its checkpoint) and removes that turn and everything after it: logs,
// results, later checkpoints and summaries that cover them.
func RestoreBeforeTurn(d *sql.DB, sessionID int64, turn int) error {
	cp, err := GetCheckpoint(d, sessionID, turn)
	if err != nil {
		return err
	}
	var snap struct {
		NPCSheets []snapshotSheet `json:"npc_sheets"`
	}
	if err := json.Unmarshal([]byte(cp.SnapshotJSON), &snap); err != nil {
		return fmt.Errorf("RestoreBeforeTurn: snapshot: %w", err)
	}

	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM npc_sheets WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	for _, ss := range snap.NPCSheets {
		s := ss.sheet()
		s.SessionID = sessionID
		if err := upsertNPCSheet(tx, s); err != nil {
			return err
		}
	}
	if err := deleteTurnsFrom(tx, sessionID, turn, true); err != nil {
		return err
	}
	return tx.Commit()
}

// snapshotSheet reads an NPC sheet from a checkpoint. Checkpoints written
// before NPCSheet had JSON tags use the Go field names (PositionY, YAMLBlob…);
// both spellings are accepted.
type snapshotSheet struct {
	NPCSheet
	OldHPMax     int    `json:"HPMax"`
	OldMPMax     int    `json:"MPMax"`
	OldPositionX int    `json:"PositionX"`
	OldPositionY int    `json:"PositionY"`
	OldYAMLBlob  string `json:"YAMLBlob"`
}

func (s snapshotSheet) sheet() NPCSheet {
	out := s.NPCSheet
	if out.HPMax == 0 {
		out.HPMax = s.OldHPMax
	}
	if out.MPMax == 0 {
		out.MPMax = s.OldMPMax
	}
	if out.PositionX == 0 {
		out.PositionX = s.OldPositionX
	}
	if out.PositionY == 0 {
		out.PositionY = s.OldPositionY
	}
	if out.YAMLBlob == "" {
		out.YAMLBlob = s.OldYAMLBlob
	}
	return out
}

// DeleteTurnsFrom removes turn and later from logs, results, checkpoints and
// summaries without touching NPC sheets (used when a turn is abandoned before
// it changed any state).
func DeleteTurnsFrom(d *sql.DB, sessionID int64, turn int) error {
	return deleteTurnsFrom(d, sessionID, turn, true)
}

func deleteTurnsFrom(d execer, sessionID int64, turn int, withCheckpoints bool) error {
	stmts := []string{
		`DELETE FROM session_logs WHERE session_id = ? AND turn >= ?`,
		`DELETE FROM turn_results WHERE session_id = ? AND turn >= ?`,
		`DELETE FROM session_summaries WHERE session_id = ? AND upto_turn >= ?`,
	}
	if withCheckpoints {
		stmts = append(stmts, `DELETE FROM checkpoints WHERE session_id = ? AND turn >= ?`)
	}
	for _, q := range stmts {
		if _, err := d.Exec(q, sessionID, turn); err != nil {
			return err
		}
	}
	return nil
}

// ── 要約 ─────────────────────────────────────────────────────────────────────

// AddSummary stores summary sentences covering the session up to uptoTurn.
func AddSummary(d *sql.DB, sessionID int64, uptoTurn int, sentences []string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, s := range sentences {
		if _, err := tx.Exec(`INSERT INTO session_summaries (session_id, upto_turn, text) VALUES (?, ?, ?)`,
			sessionID, uptoTurn, s); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListSummary returns all summary sentences of a session in order.
func ListSummary(d *sql.DB, sessionID int64) ([]string, error) {
	rows, err := d.Query(`SELECT text FROM session_summaries WHERE session_id = ? ORDER BY upto_turn, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SummarizedUpto returns the last turn covered by the summary (0 when none).
func SummarizedUpto(d *sql.DB, sessionID int64) (int, error) {
	var n sql.NullInt64
	err := d.QueryRow(`SELECT MAX(upto_turn) FROM session_summaries WHERE session_id = ?`, sessionID).Scan(&n)
	return int(n.Int64), err
}
