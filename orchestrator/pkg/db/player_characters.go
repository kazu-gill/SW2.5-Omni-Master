package db

import (
	"database/sql"
	"fmt"
	"time"
)

type PlayerCharacter struct {
	ID        int64
	Name      string
	JSONBlob  string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func ListPlayerCharacters(d *sql.DB) ([]PlayerCharacter, error) {
	rows, err := d.Query(
		`SELECT id, name, json_blob, is_active, created_at, updated_at
		 FROM player_characters ORDER BY is_active DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlayerCharacter
	for rows.Next() {
		var pc PlayerCharacter
		if err := rows.Scan(&pc.ID, &pc.Name, &pc.JSONBlob, &pc.IsActive, &pc.CreatedAt, &pc.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pc)
	}
	return out, rows.Err()
}

func GetPlayerCharacter(d *sql.DB, id int64) (*PlayerCharacter, error) {
	row := d.QueryRow(
		`SELECT id, name, json_blob, is_active, created_at, updated_at
		 FROM player_characters WHERE id = ?`, id)
	var pc PlayerCharacter
	if err := row.Scan(&pc.ID, &pc.Name, &pc.JSONBlob, &pc.IsActive, &pc.CreatedAt, &pc.UpdatedAt); err != nil {
		return nil, fmt.Errorf("GetPlayerCharacter(%d): %w", id, err)
	}
	return &pc, nil
}

func CreatePlayerCharacter(d *sql.DB, name, jsonBlob string) (*PlayerCharacter, error) {
	res, err := d.Exec(
		`INSERT INTO player_characters (name, json_blob) VALUES (?, ?)`, name, jsonBlob)
	if err != nil {
		return nil, fmt.Errorf("CreatePlayerCharacter: %w", err)
	}
	id, _ := res.LastInsertId()
	return GetPlayerCharacter(d, id)
}

func UpdatePlayerCharacter(d *sql.DB, id int64, name, jsonBlob string) error {
	_, err := d.Exec(
		`UPDATE player_characters SET name = ?, json_blob = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`, name, jsonBlob, id)
	return err
}

func DeletePlayerCharacter(d *sql.DB, id int64) error {
	_, err := d.Exec(`DELETE FROM player_characters WHERE id = ?`, id)
	return err
}

// ActivatePlayerCharacter sets the given PC as active and deactivates all others.
func ActivatePlayerCharacter(d *sql.DB, id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`UPDATE player_characters SET is_active = 0`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE player_characters SET is_active = 1 WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func DeactivatePlayerCharacter(d *sql.DB, id int64) error {
	_, err := d.Exec(`UPDATE player_characters SET is_active = 0 WHERE id = ?`, id)
	return err
}
