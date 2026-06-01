package db

import (
	"database/sql"

	_ "modernc.org/sqlite" // registers "sqlite" driver
)

// Open opens (or creates) a SQLite database at path.
func Open(path string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// CreateSchema creates all pipeline tables. Safe to call multiple times.
func CreateSchema(conn *sql.DB) error {
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS raw_opensanctions (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL,
			aliases       TEXT,
			country       TEXT,
			sanctioned_ua INTEGER NOT NULL DEFAULT 0,
			decree        TEXT,
			sanction_date TEXT
		);
		CREATE TABLE IF NOT EXISTS raw_kse (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			company_name TEXT NOT NULL,
			status       TEXT,
			last_updated TEXT
		);
		CREATE TABLE IF NOT EXISTS raw_brands (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			brand_name   TEXT NOT NULL,
			company_name TEXT NOT NULL,
			source       TEXT,
			UNIQUE(brand_name, company_name)
		);
		CREATE TABLE IF NOT EXISTS companies (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL,
			aliases       TEXT,
			russia_status TEXT,
			sanctioned_ua INTEGER NOT NULL DEFAULT 0,
			decree        TEXT,
			sanction_date TEXT,
			brands        TEXT,
			sources       TEXT
		);
	`)
	return err
}
