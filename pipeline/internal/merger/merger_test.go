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

func TestResolveBrands_ExactMatch(t *testing.T) {
	pairs := []brandPair{{brand: "KitKat", owner: "Nestle"}}
	got := resolveBrands(pairs, []string{"nestle"})
	brands, ok := got["nestle"]
	if !ok {
		t.Fatal("expected brand assigned to 'nestle', got nothing")
	}
	if !contains(brands, "KitKat") {
		t.Errorf("brands = %v, want KitKat", brands)
	}
}

func TestResolveBrands_FuzzyMatch(t *testing.T) {
	pairs := []brandPair{{brand: "Gillette", owner: "Procter and Gamble"}}
	got := resolveBrands(pairs, []string{"procter gamble"})
	brands, ok := got["procter gamble"]
	if !ok {
		t.Fatal("expected brand assigned via fuzzy match to 'procter gamble', got nothing")
	}
	if !contains(brands, "Gillette") {
		t.Errorf("brands = %v, want Gillette", brands)
	}
}

func TestResolveBrands_EmptyOwnerSkipped(t *testing.T) {
	pairs := []brandPair{{brand: "Pampers", owner: "P&G"}}
	got := resolveBrands(pairs, []string{"procter gamble"})
	if len(got) != 0 {
		t.Errorf("expected no brands assigned for empty owner, got %v", got)
	}
}

func TestResolveBrands_NoMatchBelowThreshold(t *testing.T) {
	pairs := []brandPair{{brand: "SomeBrand", owner: "xyz corp"}}
	got := resolveBrands(pairs, []string{"unrelated company"})
	if len(got) != 0 {
		t.Errorf("expected no brands assigned below threshold, got %v", got)
	}
}

func TestIsConsumer(t *testing.T) {
	cases := []struct {
		industry string
		want     bool
	}{
		{"", true},
		{"Consumer Staples", true},
		{"Consumer Discretionary", true},
		{"Financials", false},
		{"Energy", false},
		{"Industrials", false},
		{"Information Technology", false},
		{"Utilities", false},
	}
	for _, c := range cases {
		got := isConsumer(c.industry)
		if got != c.want {
			t.Errorf("isConsumer(%q) = %v, want %v", c.industry, got, c.want)
		}
	}
}

func TestMerge_IndustryFilter(t *testing.T) {
	conn := tempDB(t)

	// Financials company — must be filtered out
	mustExec(t, conn, `INSERT INTO raw_kse (company_name, status, industry)
		VALUES ('Sberbank', 'Operating', 'Financials')`)

	// Consumer Staples company — must pass through
	mustExec(t, conn, `INSERT INTO raw_kse (company_name, status, industry)
		VALUES ('Nestlé', 'Reduced Operations', 'Consumer Staples')`)

	if err := Merge(conn, nil); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var sberbankCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM companies WHERE name = 'Sberbank'`).Scan(&sberbankCount); err != nil {
		t.Fatal(err)
	}
	if sberbankCount != 0 {
		t.Errorf("Sberbank (Financials) should be filtered, got %d rows", sberbankCount)
	}

	var nestleCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM companies WHERE name = 'Nestlé'`).Scan(&nestleCount); err != nil {
		t.Fatal(err)
	}
	if nestleCount != 1 {
		t.Errorf("Nestlé (Consumer Staples) should appear in companies, got %d rows", nestleCount)
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
