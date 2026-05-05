package db

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// Open opens (or creates) the SQLite database at path and applies the schema.
// Pass ":memory:" for an in-memory database (tests).
func Open(path string) (*sql.DB, error) {
	dsn := path
	if path != ":memory:" {
		dsn = path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)"
	}
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db.Open: %w", err)
	}
	d.SetMaxOpenConns(1) // SQLite is single-writer
	if err := applySchema(d); err != nil {
		d.Close()
		return nil, err
	}
	if err := migrateSchema(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// migrateSchema adds columns introduced after the initial schema.
// SQLite does not support ALTER TABLE ADD COLUMN IF NOT EXISTS, so we
// attempt each migration and ignore "duplicate column name" errors.
func migrateSchema(d *sql.DB) error {
	migrations := []string{
		`ALTER TABLE rag_chunks ADD COLUMN tag TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE rag_chunks ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE rag_chunks ADD COLUMN overrides_id INTEGER REFERENCES rag_chunks(id)`,
		`ALTER TABLE rag_chunks ADD COLUMN opt_status TEXT DEFAULT NULL`,
	}
	for _, stmt := range migrations {
		if _, err := d.Exec(stmt); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("migration %q: %w", stmt, err)
			}
		}
	}
	return nil
}

func applySchema(d *sql.DB) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	if _, err := d.Exec(string(schema)); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
