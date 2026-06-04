package exporter

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pipeline/internal/db"
)

func TestExport(t *testing.T) {
	conn := tempDB(t)

	// seed a company with all fields
	_, err := conn.Exec(`
		INSERT INTO companies (id, name, aliases, russia_status, sanctioned_ua, brands, sources)
		VALUES ('gazprom', 'Gazprom', '["Газпром"]', 'Operating', 1, '["Gazprom Neft"]', '["KSE","OpenSanctions"]')
	`)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := Export(conn, dir); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// verify companies.json.gz is readable and contains the company
	f, err := os.Open(filepath.Join(dir, "companies.json.gz"))
	if err != nil {
		t.Fatalf("open companies.json.gz: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	var out Output
	if err := json.NewDecoder(gr).Decode(&out); err != nil {
		t.Fatalf("decode companies.json.gz: %v", err)
	}
	if len(out.Companies) != 1 {
		t.Errorf("got %d companies, want 1", len(out.Companies))
	}

	c := out.Companies[0]
	if c.ID != "gazprom" {
		t.Errorf("id = %q, want gazprom", c.ID)
	}
	if c.RussiaStatus != "Operating" {
		t.Errorf("russia_status = %q, want Operating", c.RussiaStatus)
	}
	if !c.SanctionedUA {
		t.Error("sanctioned_ua = false, want true")
	}
	if len(c.Brands) != 1 || c.Brands[0] != "Gazprom Neft" {
		t.Errorf("brands = %v, want [Gazprom Neft]", c.Brands)
	}

	// verify aliases were parsed from JSON
	if len(c.Aliases) != 1 || c.Aliases[0] != "Газпром" {
		t.Errorf("aliases = %v, want [Газпром]", c.Aliases)
	}

	// verify NULL decree and sanction_date produce nil pointers
	if c.Decree != nil {
		t.Errorf("decree = %v, want nil", c.Decree)
	}
	if c.SanctionDate != nil {
		t.Errorf("sanction_date = %v, want nil", c.SanctionDate)
	}

	// verify version.json
	vData, err := os.ReadFile(filepath.Join(dir, "version.json"))
	if err != nil {
		t.Fatalf("read version.json: %v", err)
	}
	var v Version
	if err := json.Unmarshal(vData, &v); err != nil {
		t.Fatalf("parse version.json: %v", err)
	}
	if v.Records != 1 {
		t.Errorf("records = %d, want 1", v.Records)
	}
	if v.Version == "" {
		t.Error("version is empty")
	}
}

func TestExportIncludesBarcodes(t *testing.T) {
	conn := tempDB(t)
	_, err := conn.Exec(`
		INSERT INTO companies (id, name, brands, sources)
		VALUES ('pepsi', 'PepsiCo', '["Pepsi"]', '["KSE"]')
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(`INSERT INTO raw_barcodes (code, brand) VALUES ('0012000001253', 'Pepsi')`)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := Export(conn, dir); err != nil {
		t.Fatalf("Export: %v", err)
	}

	f, err := os.Open(filepath.Join(dir, "companies.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, _ := gzip.NewReader(f)
	defer gr.Close()

	var out Output
	if err := json.NewDecoder(gr).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Barcodes) != 1 {
		t.Fatalf("barcodes count = %d, want 1", len(out.Barcodes))
	}
	if out.Barcodes[0].Code != "0012000001253" {
		t.Errorf("code = %q, want 0012000001253", out.Barcodes[0].Code)
	}
	if out.Barcodes[0].Brand != "Pepsi" {
		t.Errorf("brand = %q, want Pepsi", out.Barcodes[0].Brand)
	}
}

func tempDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "exporter-test-*.db")
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
