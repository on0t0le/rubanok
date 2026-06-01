package merger

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"pipeline/internal/db"
)

func TestMerge_MatchedCompany(t *testing.T) {
	conn := tempDB(t)

	// seed OS entity
	mustExec(t, conn, `INSERT INTO raw_opensanctions (id, name, aliases, country, sanctioned_ua)
		VALUES ('os-1', 'Gazprom', '["ПАО Газпром"]', 'ru', 1)`)

	// seed KSE with same name — guaranteed 100 score match
	mustExec(t, conn, `INSERT INTO raw_kse (company_name, status, last_updated)
		VALUES ('Gazprom', 'Operating', '2024-01-01')`)

	// seed a brand mapped to the same company name
	mustExec(t, conn, `INSERT INTO raw_brands (brand_name, company_name, source)
		VALUES ('Gazprom Neft', 'Gazprom', 'manual')`)

	if err := Merge(conn, nil); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var russiaStatus, sourcesJSON, brandsJSON string
	var sanctionedUA int
	err := conn.QueryRow(`
		SELECT russia_status, sanctioned_ua, brands, sources
		FROM companies WHERE name = 'Gazprom'
	`).Scan(&russiaStatus, &sanctionedUA, &brandsJSON, &sourcesJSON)
	if err != nil {
		t.Fatalf("query merged company: %v", err)
	}

	if russiaStatus != "Operating" {
		t.Errorf("russia_status = %q, want Operating", russiaStatus)
	}
	if sanctionedUA != 1 {
		t.Errorf("sanctioned_ua = %d, want 1", sanctionedUA)
	}

	var sources []string
	if err := json.Unmarshal([]byte(sourcesJSON), &sources); err != nil {
		t.Fatalf("parse sources: %v", err)
	}
	if !contains(sources, "KSE") || !contains(sources, "OpenSanctions") {
		t.Errorf("sources = %v, want both KSE and OpenSanctions", sources)
	}

	var brands []string
	if err := json.Unmarshal([]byte(brandsJSON), &brands); err != nil {
		t.Fatalf("parse brands: %v", err)
	}
	if len(brands) == 0 {
		t.Error("brands is empty, want at least 1 entry")
	}
}

func TestMerge_Override(t *testing.T) {
	conn := tempDB(t)

	mustExec(t, conn, `INSERT INTO raw_opensanctions (id, name, aliases, country, sanctioned_ua)
		VALUES ('os-nestle', 'Nestlé S.A.', '[]', 'ch', 0)`)
	mustExec(t, conn, `INSERT INTO raw_kse (company_name, status)
		VALUES ('Nestle', 'Operating')`)

	overrides := []Override{{KSEName: "Nestle", OpenSanctionsName: "Nestlé S.A."}}
	if err := Merge(conn, overrides); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var russiaStatus string
	if err := conn.QueryRow(`SELECT russia_status FROM companies WHERE name = 'Nestlé S.A.'`).
		Scan(&russiaStatus); err != nil {
		t.Fatalf("query: %v", err)
	}
	if russiaStatus != "Operating" {
		t.Errorf("russia_status = %q, want Operating (override must have matched)", russiaStatus)
	}
}

func TestMerge_KSEOnly(t *testing.T) {
	conn := tempDB(t)

	// KSE entry with no matching OS entity
	mustExec(t, conn, `INSERT INTO raw_kse (company_name, status) VALUES ('UnknownCorp', 'Exited')`)

	if err := Merge(conn, nil); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM companies WHERE name = 'UnknownCorp'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("got %d rows for KSE-only company, want 1", count)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func mustExec(t *testing.T, conn *sql.DB, query string) {
	t.Helper()
	if _, err := conn.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func tempDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "merger-test-*.db")
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
