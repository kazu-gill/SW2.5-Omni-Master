package db

import (
	"database/sql"
	"fmt"
	"time"
)

type Session struct {
	ID        int64
	QuestID   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func CreateSession(d *sql.DB, questID string) (*Session, error) {
	res, err := d.Exec(`INSERT INTO sessions (quest_id) VALUES (?)`, questID)
	if err != nil {
		return nil, fmt.Errorf("CreateSession: %w", err)
	}
	id, _ := res.LastInsertId()
	return GetSession(d, id)
}

func GetSession(d *sql.DB, id int64) (*Session, error) {
	row := d.QueryRow(`SELECT id, quest_id, created_at, updated_at FROM sessions WHERE id = ?`, id)
	s := &Session{}
	if err := row.Scan(&s.ID, &s.QuestID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, fmt.Errorf("GetSession(%d): %w", id, err)
	}
	return s, nil
}

func TouchSession(d *sql.DB, id int64) error {
	_, err := d.Exec(`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}

type NPCSheet struct {
	ID        int64
	SessionID int64
	Name      string
	HP        int
	MP        int
	PositionX int
	PositionY int
	YAMLBlob  string
	UpdatedAt time.Time
}

func UpsertNPCSheet(d *sql.DB, s NPCSheet) error {
	_, err := d.Exec(`
		INSERT INTO npc_sheets (session_id, name, hp, mp, position_x, position_y, yaml_blob)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, name) DO UPDATE SET
			hp = excluded.hp, mp = excluded.mp,
			position_x = excluded.position_x, position_y = excluded.position_y,
			yaml_blob = excluded.yaml_blob, updated_at = CURRENT_TIMESTAMP`,
		s.SessionID, s.Name, s.HP, s.MP, s.PositionX, s.PositionY, s.YAMLBlob,
	)
	return err
}

func GetNPCSheets(d *sql.DB, sessionID int64) ([]NPCSheet, error) {
	rows, err := d.Query(
		`SELECT id, session_id, name, hp, mp, position_x, position_y, yaml_blob, updated_at
		 FROM npc_sheets WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NPCSheet
	for rows.Next() {
		var s NPCSheet
		if err := rows.Scan(&s.ID, &s.SessionID, &s.Name, &s.HP, &s.MP,
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
