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

func TestFetchPersonNames(t *testing.T) {
	fixture := `{"results":{"bindings":[
		{"name":{"type":"literal","xml:lang":"en","value":"Jamie Oliver"}}
	]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		w.Write([]byte(fixture))
	}))
	defer srv.Close()

	old := wikidataEndpoint
	wikidataEndpoint = srv.URL
	defer func() { wikidataEndpoint = old }()

	result, err := FetchPersonNames([]string{"Jamie Oliver", "PepsiCo"})
	if err != nil {
		t.Fatalf("FetchPersonNames: %v", err)
	}
	if !result["Jamie Oliver"] {
		t.Error("expected Jamie Oliver to be detected as person")
	}
	if result["PepsiCo"] {
		t.Error("PepsiCo should not be detected as person")
	}
	if len(result) != 1 {
		t.Errorf("got %d persons, want 1", len(result))
	}
}

func TestFetchPersonNames_Empty(t *testing.T) {
	result, err := FetchPersonNames([]string{})
	if err != nil {
		t.Fatalf("FetchPersonNames: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d results for empty input, want 0", len(result))
	}
}

func TestFetchPersonNames_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	old := wikidataEndpoint
	wikidataEndpoint = srv.URL
	defer func() { wikidataEndpoint = old }()

	result, err := FetchPersonNames([]string{"Jamie Oliver"})
	if err == nil {
		t.Error("expected error for HTTP 500, got nil")
	}
	if len(result) != 0 {
		t.Errorf("got %d results on error, want 0", len(result))
	}
}

func TestFetchPersonNames_NoPersons(t *testing.T) {
	fixture := `{"results":{"bindings":[]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		w.Write([]byte(fixture))
	}))
	defer srv.Close()

	old := wikidataEndpoint
	wikidataEndpoint = srv.URL
	defer func() { wikidataEndpoint = old }()

	result, err := FetchPersonNames([]string{"PepsiCo", "Nestlé"})
	if err != nil {
		t.Fatalf("FetchPersonNames: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d persons, want 0", len(result))
	}
}
