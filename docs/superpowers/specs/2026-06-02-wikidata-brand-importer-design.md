# Wikidata Brand Importer Design

**Date:** 2026-06-02
**Scope:** Automatically populate brand→company mappings from Wikidata SPARQL, replacing manual `brands.json` maintenance for long-tail brands.
**Goal:** Searching "Jameson", "Absolut", or any Wikidata-catalogued brand resolves to its parent company's Russia status without manual entry.

---

## Architecture

One new file: `pipeline/internal/importer/wikidata.go`

No schema changes. No new tables. Inserts into existing `raw_brands (brand_name, company_name, source)` with `source = "wikidata"`. Existing `resolveBrands()` fuzzy-matching in merger handles all downstream resolution — no merger changes needed.

**Import order in `main.go`:**
1. Manual `brands.json` (`source = "manual"`) — curated, highest priority
2. Wikidata (`source = "wikidata"`) — structured, authoritative
3. Open Food Facts (`source = "open_food_facts"`) — broad but noisy

`INSERT OR IGNORE` on `UNIQUE(brand_name, company_name)` constraint ensures manual entries are never overwritten.

---

## SPARQL Query

```sparql
SELECT DISTINCT ?brandLabel ?ownerLabel WHERE {
  ?brand wdt:P31 wd:Q1331049 ;
         wdt:P127 ?owner .
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en" }
}
LIMIT 50000
```

- `P31 wd:Q1331049` — instance of "brand" (keeps scope manageable, ~5k–20k pairs)
- `P127` — "owned by" (brand → parent company)
- `SERVICE wikibase:label` — resolves QIDs to English labels
- `LIMIT 50000` — prevents timeout on large result sets

Endpoint: `https://query.wikidata.org/sparql` via HTTP POST.
Accept header: `application/sparql-results+json`.
Timeout: 60 seconds.

---

## Data Flow

```
ImportBrandsFromWikidata(conn *sql.DB) error

1. POST SPARQL query to Wikidata endpoint (60s timeout)
2. Parse JSON: results.bindings[i].brandLabel.value + ownerLabel.value
3. Skip row if either label is empty or looks like a QID ("Q" followed by digits)
4. INSERT OR IGNORE into raw_brands (brand_name, company_name, 'wikidata')
5. Return nil on success
```

---

## Error Handling

All errors are non-fatal. Manual `brands.json` always runs first, so curated brands (Jameson, Oreo, etc.) resolve correctly even if Wikidata is unreachable.

| Failure | Behaviour |
|---------|-----------|
| Network error / HTTP non-200 | `WARN: wikidata import failed: <err>`, return `nil` |
| Individual row parse error | Skip row, continue |
| Query timeout (>60s) | Same as network error |

---

## New File

**`pipeline/internal/importer/wikidata.go`**

```
package importer

func ImportBrandsFromWikidata(conn *sql.DB) error
func queryWikidata(client *http.Client) ([]wikidataBrand, error)  // unexported, testable
func parseWikidataJSON(r io.Reader) ([]wikidataBrand, error)      // unexported, testable

type wikidataBrand struct {
    Brand string
    Owner string
}
```

---

## Testing

**`pipeline/internal/importer/wikidata_test.go`**

Fixture: `testdata/wikidata_response.json` — minimal SPARQL JSON with:
- 2 valid brand→owner rows
- 1 row where label is a QID (e.g. `"Q99999"`) — must be skipped
- 1 row with empty brandLabel — must be skipped

Tests:
- `TestImportBrandsFromWikidata` — `httptest.NewServer` serves fixture, asserts 2 rows in `raw_brands`, QID and empty rows absent
- `TestImportBrandsFromWikidata_NetworkError` — server returns HTTP 500, asserts function returns `nil`, 0 rows inserted
- `TestParseWikidataJSON` — unit test for parser with fixture, verifies skip logic

---

## main.go Change

Add after `ImportBrandsFromJSONPath`, before `ImportBrandsFromOpenFoodFacts`:

```go
if err := importer.ImportBrandsFromWikidata(conn); err != nil {
    log.Printf("WARN: wikidata: %v", err)
}
```

(Error already non-fatal inside the function; this guard is belt-and-suspenders.)

---

## Out of Scope

- Pagination beyond `LIMIT 50000` — query returns well under this for `instance of brand`
- Additional Wikidata properties (P176 manufacturer, P749 parent org) — P127 covers consumer brands adequately for MVP
- Caching Wikidata response between runs — pipeline rebuilds SQLite from scratch each run anyway
- Language variants beyond English labels — Wikidata fallback QID filtering handles unlabeled items
