package importer

import (
	"database/sql"
	"os"
	"testing"

	"pipeline/internal/db"
)

// tempDB creates a temporary SQLite database with pipeline schema.
// Automatically cleaned up when the test ends.
func tempDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "importer-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	conn, err := db.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := db.CreateSchema(conn); err != nil {
		t.Fatal(err)
	}
	return conn
}
