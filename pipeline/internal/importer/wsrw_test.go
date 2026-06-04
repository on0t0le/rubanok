package importer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractWSRWBrands_PureLatin(t *testing.T) {
	got := extractWSRWBrands("Heinz, Kraft, ABC")
	want := []string{"Heinz", "Kraft", "ABC"}
	if !equalSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractWSRWBrands_MixedCyrillic(t *testing.T) {
	// "шампуні та гелі для душу Schwarzkopf" → ["Schwarzkopf"]
	got := extractWSRWBrands("шампуні та гелі для душу Schwarzkopf")
	if len(got) != 1 || got[0] != "Schwarzkopf" {
		t.Errorf("got %v, want [Schwarzkopf]", got)
	}
}

func TestExtractWSRWBrands_Empty(t *testing.T) {
	if extractWSRWBrands("") != nil {
		t.Error("empty string should return nil")
	}
}

func TestExtractWSRWBrands_MixedCommaSeparated(t *testing.T) {
	// Some entries from WSRW combine pure-Latin and Cyrillic-mixed
	got := extractWSRWBrands("Fa, крем для обличчя Nivea")
	if !equalSlices(got, []string{"Fa", "Nivea"}) {
		t.Errorf("got %v, want [Fa Nivea]", got)
	}
}

func TestResolveWSRWStatus(t *testing.T) {
	if s := resolveWSRWStatus([]int{25}); s != "Operating" {
		t.Errorf("got %q, want Operating", s)
	}
	if s := resolveWSRWStatus([]int{26}); s != "Bypassing" {
		t.Errorf("got %q, want Bypassing", s)
	}
	if s := resolveWSRWStatus([]int{27}); s != "Tacking" {
		t.Errorf("got %q, want Tacking", s)
	}
	if s := resolveWSRWStatus(nil); s != "Unknown" {
		t.Errorf("got %q, want Unknown", s)
	}
}

func TestImportFromWSRW_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-WP-TotalPages", "1")
		w.Header().Set("Content-Type", "application/json")
		companies := []wsrwCompany{
			{
				Slug:      "henkel",
				Title:     struct{ Rendered string `json:"rendered"` }{Rendered: "Henkel"},
				StatusIDs: []int{25},
				ACF:       struct{ Brands string `json:"brands"` }{Brands: "Fa, Schwarzkopf, Persil"},
			},
		}
		json.NewEncoder(w).Encode(companies)
	}))
	defer srv.Close()

	// Swap base URL to test server
	orig := wsrwClient
	wsrwClient = srv.Client()
	defer func() { wsrwClient = orig }()

	// Monkey-patch fetch URL by replacing the fetch function used in import
	// We test importFromWSRWURL directly to inject the mock URL.
	conn := tempDB(t)
	if err := importFromWSRWURL(conn, srv.URL+"/companies"); err != nil {
		t.Fatalf("importFromWSRWURL: %v", err)
	}

	var name, status, brands string
	if err := conn.QueryRow(`SELECT name, status, brands FROM raw_wsrw WHERE slug = 'henkel'`).
		Scan(&name, &status, &brands); err != nil {
		t.Fatalf("query: %v", err)
	}
	if name != "Henkel" {
		t.Errorf("name = %q, want Henkel", name)
	}
	if status != "Operating" {
		t.Errorf("status = %q, want Operating", status)
	}
	if brands == "" {
		t.Error("brands should not be empty")
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
