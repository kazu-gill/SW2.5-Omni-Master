package db

import (
	"database/sql"
	"fmt"
)

type Scenario struct {
	ID              int64
	Title           string
	Rank            string
	Summary         string
	Description     string
	Client          string
	Reward          string
	Target          string
	Level           string
	LocationsJSON   string
	EnemiesJSON     string
	PlotHooksJSON   string
	EventsJSON      string
	Status          string
	GeneratedAt     string
}

type ScenarioImage struct {
	ID         int64
	ScenarioID *int64
	Category   string
	Label      string
	FilePath   string
	Width      int
	Height     int
}

func ListScenarios(d *sql.DB, status string) ([]Scenario, error) {
	q := `SELECT id, title, rank, summary, description, client, reward, target, level,
	             locations_json, enemies_json, plot_hooks_json, events_json, status, generated_at
	      FROM scenarios`
	args := []any{}
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scenario
	for rows.Next() {
		var s Scenario
		if err := rows.Scan(&s.ID, &s.Title, &s.Rank, &s.Summary, &s.Description,
			&s.Client, &s.Reward, &s.Target, &s.Level,
			&s.LocationsJSON, &s.EnemiesJSON, &s.PlotHooksJSON, &s.EventsJSON,
			&s.Status, &s.GeneratedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func GetScenario(d *sql.DB, id int64) (*Scenario, error) {
	row := d.QueryRow(`
		SELECT id, title, rank, summary, description, client, reward, target, level,
		       locations_json, enemies_json, plot_hooks_json, events_json, status, generated_at
		FROM scenarios WHERE id = ?`, id)
	var s Scenario
	if err := row.Scan(&s.ID, &s.Title, &s.Rank, &s.Summary, &s.Description,
		&s.Client, &s.Reward, &s.Target, &s.Level,
		&s.LocationsJSON, &s.EnemiesJSON, &s.PlotHooksJSON, &s.EventsJSON,
		&s.Status, &s.GeneratedAt); err != nil {
		return nil, fmt.Errorf("GetScenario(%d): %w", id, err)
	}
	return &s, nil
}

func ListScenarioImages(d *sql.DB, scenarioID int64) ([]ScenarioImage, error) {
	rows, err := d.Query(`
		SELECT id, scenario_id, category, label, file_path, width, height
		FROM scenario_images WHERE scenario_id = ? ORDER BY id`, scenarioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScenarioImage
	for rows.Next() {
		var img ScenarioImage
		var sid sql.NullInt64
		if err := rows.Scan(&img.ID, &sid, &img.Category, &img.Label,
			&img.FilePath, &img.Width, &img.Height); err != nil {
			return nil, err
		}
		if sid.Valid {
			v := sid.Int64
			img.ScenarioID = &v
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

func ListBackgroundImages(d *sql.DB) ([]ScenarioImage, error) {
	rows, err := d.Query(`
		SELECT id, scenario_id, category, label, file_path, width, height
		FROM scenario_images WHERE scenario_id IS NULL ORDER BY label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScenarioImage
	for rows.Next() {
		var img ScenarioImage
		var sid sql.NullInt64
		if err := rows.Scan(&img.ID, &sid, &img.Category, &img.Label,
			&img.FilePath, &img.Width, &img.Height); err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

func UpdateScenarioStatus(d *sql.DB, id int64, status string) error {
	_, err := d.Exec(`UPDATE scenarios SET status=? WHERE id=?`, status, id)
	return err
}
