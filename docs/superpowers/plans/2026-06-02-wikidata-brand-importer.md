# Wikidata Brand Importer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically populate brand→company mappings from Wikidata SPARQL so Ukrainian grocery-store brands resolve to their parent company's Russia status without manual maintenance.

**Architecture:** One new file `pipeline/internal/importer/wikidata.go` POSTs a SPARQL query to `https://query.wikidata.org/sparql`, parses the JSON response, and inserts brand→owner pairs into the existing `raw_brands` table (`source = "wikidata"`). The existing `resolveBrands()` fuzzy-matcher in the merger handles all downstream resolution — no merger changes needed. `main.go` calls `ImportBrandsFromWikidata` after manual `brands.json` (so `INSERT OR IGNORE` preserves curated entries) and before Open Food Facts.

**Tech Stack:** Go, `net/http`, `net/http/httptest` (tests), `regexp`, existing `raw_brands` SQLite table

---

## File Map

| File | Change |
|------|--------|
| `pipeline/internal/importer/wikidata.go` | Create: SPARQL fetch + parse + insert |
| `pipeline/internal/importer/testdata/wikidata_response.json` | Create: SPARQL JSON fixture |
| `pipeline/internal/importer/wikidata_test.go` | Create: 3 tests |
| `pipeline/cmd/pipeline/main.go` | Modify: add `ImportBrandsFromWikidata` call in `newImportBrandsCmd` |

---

## Task 1: Wikidata importer — implementation + tests

**Files:**
- Create: `pipeline/internal/importer/wikidata.go`
- Create: `pipeline/internal/importer/testdata/wikidata_response.json`
- Create: `pipeline/internal/importer/wikidata_test.go`

- [ ] **Step 1: Create test fixture**

Create `pipeline/internal/importer/testdata/wikidata_response.json`:

```json
{
  "head": { "vars": ["brandLabel", "ownerLabel"] },
  "results": {
    "bindings": [
      {
        "brandLabel": { "type": "literal", "value": "Pepsi" },
        "ownerLabel": { "type": "literal", "value": "PepsiCo" }
      },
      {
        "brandLabel": { "type": "literal", "value": "KitKat" },
        "ownerLabel": { "type": "literal", "value": "Nestlé" }
      },
      {
        "brandLabel": { "type": "literal", "value": "Q99999" },
        "ownerLabel": { "type": "literal", "value": "Some Company" }
      },
      {
        "brandLabel": { "type": "literal", "value": "" },
        "ownerLabel": { "type": "literal", "value": "Other Company" }
      }
    ]
  }
}
```

Rows 1-2 are valid. Row 3 has a QID as the brand label (must be skipped). Row 4 has an empty brand label (must be skipped). Only 2 rows should be inserted.

- [ ] **Step 2: Write failing tests**

Create `pipeline/internal/importer/wikidata_test.go`:

```go
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
```

- [ ] **Step 3: Run to verify they fail**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/importer/ -run "TestImportBrandsFromWikidata|TestParseWikidataJSON" -v 2>&1 | tail -15
```

Expected: compile error — `undefined: wikidataEndpoint`, `undefined: ImportBrandsFromWikidata`, `undefined: parseWikidataJSON`

- [ ] **Step 4: Create `wikidata.go`**

Create `pipeline/internal/importer/wikidata.go`:

```go
package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var wikidataClient = &http.Client{Timeout: 60 * time.Second}
var wikidataEndpoint = "https://query.wikidata.org/sparql"

const wikidataSPARQL = `
SELECT DISTINCT ?brandLabel ?ownerLabel WHERE {
  ?brand wdt:P31 wd:Q1331049 ;
         wdt:P127 ?owner .
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en" }
}
LIMIT 50000
`

var qidRE = regexp.MustCompile(`^Q\d+$`)

type wikidataBrand struct {
	Brand string
	Owner string
}

type wikidataResponse struct {
	Results struct {
		Bindings []struct {
			BrandLabel struct{ Value string } `json:"brandLabel"`
			OwnerLabel struct{ Value string } `json:"ownerLabel"`
		} `json:"bindings"`
	} `json:"results"`
}

// ImportBrandsFromWikidata fetches brand→company pairs from the Wikidata
// SPARQL endpoint and inserts them into raw_brands with source "wikidata".
func ImportBrandsFromWikidata(conn *sql.DB) error {
	brands, err := queryWikidata(wikidataClient)
	if err != nil {
		return fmt.Errorf("wikidata query: %w", err)
	}
	return insertWikidataBrands(conn, brands)
}

func queryWikidata(client *http.Client) ([]wikidataBrand, error) {
	body := url.Values{"query": {wikidataSPARQL}}.Encode()
	req, err := http.NewRequest("POST", wikidataEndpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "BrandCheckUA/1.0 (https://github.com/on0t0le/rubanok)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return parseWikidataJSON(resp.Body)
}

func parseWikidataJSON(r io.Reader) ([]wikidataBrand, error) {
	var wr wikidataResponse
	if err := json.NewDecoder(r).Decode(&wr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	var result []wikidataBrand
	for _, b := range wr.Results.Bindings {
		brand := b.BrandLabel.Value
		owner := b.OwnerLabel.Value
		if brand == "" || owner == "" {
			continue
		}
		if qidRE.MatchString(brand) || qidRE.MatchString(owner) {
			continue
		}
		result = append(result, wikidataBrand{Brand: brand, Owner: owner})
	}
	return result, nil
}

func insertWikidataBrands(conn *sql.DB, brands []wikidataBrand) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO raw_brands (brand_name, company_name, source) VALUES (?, ?, 'wikidata')`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range brands {
		if _, err := stmt.Exec(b.Brand, b.Owner); err != nil {
			return fmt.Errorf("insert %q: %w", b.Brand, err)
		}
	}
	return tx.Commit()
}
```

- [ ] **Step 5: Run all importer tests**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/importer/ -v 2>&1 | tail -20
```

Expected: all tests pass including the 3 new Wikidata tests

- [ ] **Step 6: Commit**

```bash
git -C /Users/on0t0le/projects/personal/rubanok add \
  pipeline/internal/importer/wikidata.go \
  pipeline/internal/importer/testdata/wikidata_response.json \
  pipeline/internal/importer/wikidata_test.go
git -C /Users/on0t0le/projects/personal/rubanok commit -m "feat(pipeline): Wikidata SPARQL brand importer"
```

---

## Task 2: Wire Wikidata importer into pipeline CLI

**Files:**
- Modify: `pipeline/cmd/pipeline/main.go`

- [ ] **Step 1: Update `newImportBrandsCmd` in `main.go`**

In `pipeline/cmd/pipeline/main.go`, replace the `RunE` body of `newImportBrandsCmd` from:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    conn, err := openDB()
    if err != nil {
        return err
    }
    defer conn.Close()

    fmt.Printf("importing brands from %s...\n", brandsPath)
    if err := importer.ImportBrandsFromJSONPath(conn, brandsPath); err != nil {
        return fmt.Errorf("local brands: %w", err)
    }

    fmt.Println("importing brands from Open Food Facts (this may take several minutes)...")
    return importer.ImportBrandsFromOpenFoodFacts(conn)
},
```

to:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    conn, err := openDB()
    if err != nil {
        return err
    }
    defer conn.Close()

    fmt.Printf("importing brands from %s...\n", brandsPath)
    if err := importer.ImportBrandsFromJSONPath(conn, brandsPath); err != nil {
        return fmt.Errorf("local brands: %w", err)
    }

    fmt.Println("importing brands from Wikidata...")
    if err := importer.ImportBrandsFromWikidata(conn); err != nil {
        fmt.Printf("WARN: wikidata: %v\n", err)
    }

    fmt.Println("importing brands from Open Food Facts (this may take several minutes)...")
    return importer.ImportBrandsFromOpenFoodFacts(conn)
},
```

Also update the `Short` description from:
```go
Short: "Import brand→company mappings from local JSON and Open Food Facts",
```
to:
```go
Short: "Import brand→company mappings from local JSON, Wikidata, and Open Food Facts",
```

- [ ] **Step 2: Run full test suite**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./... 2>&1 | tail -10
```

Expected: all packages pass

- [ ] **Step 3: Commit**

```bash
git -C /Users/on0t0le/projects/personal/rubanok add pipeline/cmd/pipeline/main.go
git -C /Users/on0t0le/projects/personal/rubanok commit -m "feat(pipeline): wire Wikidata brand importer into brands import step"
```
