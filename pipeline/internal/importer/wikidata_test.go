package importer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestImportBrandsFromWikidata(t *testing.T) {
	fixture, err := os.ReadFile("testdata/wikidata_response.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		w.Write(fixture)
	}))
	defer srv.Close()

	old := wikidataEndpoint
	wikidataEndpoint = srv.URL
	defer func() { wikidataEndpoint = old }()

	conn := tempDB(t)
	if err := ImportBrandsFromWikidata(conn); err != nil {
		t.Fatalf("ImportBrandsFromWikidata: %v", err)
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM raw_brands").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("got %d rows, want 2 (QID and empty rows must be skipped)", count)
	}

	var owner string
	if err := conn.QueryRow(
		"SELECT company_name FROM raw_brands WHERE brand_name = 'Pepsi'",
	).Scan(&owner); err != nil {
		t.Fatalf("query Pepsi: %v", err)
	}
	if owner != "PepsiCo" {
		t.Errorf("owner = %q, want PepsiCo", owner)
	}
}

func TestImportBrandsFromWikidata_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	old := wikidataEndpoint
	wikidataEndpoint = srv.URL
	defer func() { wikidataEndpoint = old }()

	conn := tempDB(t)
	err := ImportBrandsFromWikidata(conn)
	if err == nil {
		t.Error("expected error for HTTP 500, got nil")
	}

	var count int
	conn.QueryRow("SELECT COUNT(*) FROM raw_brands").Scan(&count)
	if count != 0 {
		t.Errorf("got %d rows, want 0 on error", count)
	}
}

func TestParseWikidataJSON(t *testing.T) {
	json := `{
		"results": { "bindings": [
			{"brandLabel":{"value":"Oreo"},"ownerLabel":{"value":"Mondelez"}},
			{"brandLabel":{"value":"Q12345"},"ownerLabel":{"value":"Someone"}},
			{"brandLabel":{"value":""},"ownerLabel":{"value":"Someone"}},
			{"brandLabel":{"value":"Valid"},"ownerLabel":{"value":"Q99999"}}
		]}
	}`
	brands, err := parseWikidataJSON(strings.NewReader(json))
	if err != nil {
		t.Fatalf("parseWikidataJSON: %v", err)
	}
	if len(brands) != 1 {
		t.Errorf("got %d brands, want 1 (only Oreo→Mondelez is valid)", len(brands))
	}
	if len(brands) > 0 && brands[0].Brand != "Oreo" {
		t.Errorf("brand = %q, want Oreo", brands[0].Brand)
	}
}
