package importer

import (
	"strings"
	"testing"
)

func TestImportKSEFromPath(t *testing.T) {
	conn := tempDB(t)
	if err := ImportKSEFromPath(conn, "testdata/kse.html"); err != nil {
		t.Fatalf("ImportKSEFromPath: %v", err)
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM raw_kse").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Errorf("got %d rows, want 6", count)
	}
}

func TestImportKSEStatusMapping(t *testing.T) {
	cases := []struct{ action, want string }{
		{"continues operating in Russia", "Operating"},
		{"fully exited Russia and closed all stores", "Exited"},
		{"suspended operations in Russia", "Suspended"},
		{"reduced product range in Russia", "Reduced Operations"},
		{"under review by board", "Unknown"},
	}
	for _, c := range cases {
		got := mapYaleStatus(c.action)
		if got != c.want {
			t.Errorf("mapYaleStatus(%q) = %q, want %q", c.action, got, c.want)
		}
	}
}

func TestImportKSESkipsEmptyName(t *testing.T) {
	conn := tempDB(t)
	if err := ImportKSEFromPath(conn, "testdata/kse.html"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM raw_kse WHERE company_name = ''").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("empty-name rows should be skipped, got %d", count)
	}
}

func TestImportKSETableNotFound(t *testing.T) {
	conn := tempDB(t)
	err := parseYaleHTML(conn, strings.NewReader("<html><body><p>No table here</p></body></html>"))
	if err == nil {
		t.Error("expected error when table not found, got nil")
	}
}

func TestImportKSESetsLastUpdated(t *testing.T) {
	conn := tempDB(t)
	if err := ImportKSEFromPath(conn, "testdata/kse.html"); err != nil {
		t.Fatal(err)
	}
	var updated string
	if err := conn.QueryRow(
		"SELECT last_updated FROM raw_kse WHERE company_name = 'IKEA'",
	).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if updated == "" {
		t.Error("last_updated should be non-empty")
	}
}

func TestImportKSEStoresIndustry(t *testing.T) {
	conn := tempDB(t)
	if err := ImportKSEFromPath(conn, "testdata/kse.html"); err != nil {
		t.Fatalf("ImportKSEFromPath: %v", err)
	}
	var industry string
	if err := conn.QueryRow(
		"SELECT COALESCE(industry, '') FROM raw_kse WHERE company_name = 'Mondelez International'",
	).Scan(&industry); err != nil {
		t.Fatal(err)
	}
	if industry != "Consumer Staples" {
		t.Errorf("industry = %q, want Consumer Staples", industry)
	}
}
