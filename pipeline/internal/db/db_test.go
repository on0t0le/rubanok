package db

import (
	"database/sql"
	"os"
	"testing"
)

func TestOpenAndCreateSchema(t *testing.T) {
	f, err := os.CreateTemp("", "pipeline-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	conn, err := Open(f.Name())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	if err := CreateSchema(conn); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}

	// verify all four tables exist
	for _, table := range []string{"raw_opensanctions", "raw_kse", "raw_brands", "companies"} {
		var name string
		err := conn.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %q was not created", table)
		} else if err != nil {
			t.Errorf("querying table %q: %v", table, err)
		}
	}
}

func TestCreateSchemaIdempotent(t *testing.T) {
	f, err := os.CreateTemp("", "pipeline-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	conn, err := Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := CreateSchema(conn); err != nil {
		t.Fatal(err)
	}
	// calling twice must not error (IF NOT EXISTS)
	if err := CreateSchema(conn); err != nil {
		t.Fatalf("second CreateSchema: %v", err)
	}
}
