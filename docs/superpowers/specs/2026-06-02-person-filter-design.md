# Wikidata Person Filter Design

**Date:** 2026-06-02
**Scope:** Automatically exclude person-brands (e.g. Jamie Oliver) from pipeline output by detecting Wikidata Q5 (human) entities among KSE company names.
**Goal:** No manual exclude lists. Person-brands never appear in iOS search results.

---

## Architecture

No schema changes. No new tables. One new function in `pipeline/internal/importer/wikidata.go`; two changes in `pipeline/internal/merger/merger.go`; cleanup in `main.go`, workflow, and `data/`.

| File | Change |
|------|--------|
| `pipeline/internal/importer/wikidata.go` | Add `FetchPersonNames(names []string) (map[string]bool, error)` |
| `pipeline/internal/importer/wikidata_test.go` | Add 3 tests for `FetchPersonNames` |
| `pipeline/internal/merger/merger.go` | Call `FetchPersonNames` after `loadKSE`; revert `Merge` sig to `(conn, overrides)` |
| `pipeline/internal/merger/merger_test.go` | Add `TestMerge_PersonExcluded`; update `Merge` call sites to remove nil excludes arg |
| `pipeline/cmd/pipeline/main.go` | Remove `--excludes` flag and `excludes` logic from `newMergeCmd` |
| `.github/workflows/update.yml` | Remove `--excludes data/exclude.json` from merge step |
| `data/exclude.json` | Delete |

---

## SPARQL Query

```sparql
SELECT DISTINCT ?name WHERE {
  VALUES ?name { "Jamie Oliver" "PepsiCo" "Nestlé" }
  ?item rdfs:label ?name@en ;
        wdt:P31 wd:Q5 .
}
```

- `VALUES ?name` — injected at runtime from the KSE company name list
- `wdt:P31 wd:Q5` — instance of human
- Returns only the subset of input names that are Wikidata humans

Names are injected as `"Name"` (double-quoted, space-separated in the VALUES block). The query reuses the existing `wikidataEndpoint` var and `wikidataClient`.

---

## New Function

```go
// FetchPersonNames queries Wikidata and returns the subset of names that are
// instances of human (Q5). Returns empty map on empty input or network error.
func FetchPersonNames(names []string) (map[string]bool, error)
```

**Algorithm:**
1. If `len(names) == 0` → return empty map, nil (no HTTP call)
2. Build SPARQL with `VALUES` clause from names slice
3. POST to `wikidataEndpoint` (reuses existing client + endpoint var)
4. Parse SPARQL JSON: collect `bindings[i].name.value`
5. Return `map[string]bool` of matched names

**Response parsing:** define a new local struct for the `?name` binding (the existing `wikidataResponse` struct has `brandLabel`/`ownerLabel` fields and cannot be reused):

```go
type personQueryResponse struct {
    Results struct {
        Bindings []struct {
            Name struct{ Value string } `json:"name"`
        } `json:"bindings"`
    } `json:"results"`
}
```

---

## Merger Changes

`Merge(conn *sql.DB, overrides []Override)` — `excludes []string` param removed.

After `loadKSE`:

```go
kseNames := make([]string, len(kseList))
for i, k := range kseList { kseNames[i] = k.name }

persons, err := importer.FetchPersonNames(kseNames)
if err != nil {
    fmt.Printf("WARN: person detection failed: %v — no persons excluded\n", err)
    persons = map[string]bool{}
}
// persons replaces the old excludeSet
```

The existing `excludeSet` write-time checks (`excludeSet[e.name]`, `excludeSet[k.name]`) remain — just renamed to use `persons`.

---

## Error Handling

| Failure | Behaviour |
|---------|-----------|
| Empty input | Return empty map, no HTTP call |
| Network error / HTTP non-200 | WARN log, return empty map (safe: no names excluded) |
| Parse error | Same as network error |

Safe default: on any failure, zero persons are excluded and pipeline output is unchanged.

---

## Cleanup

- `data/exclude.json` — deleted
- `Merge()` signature reverts to `(conn *sql.DB, overrides []Override)` — all callers updated
- `main.go` `newMergeCmd` — remove `excludesPath` var and `--excludes` flag
- Workflow merge step — remove `--excludes data/exclude.json`

---

## Testing

### `wikidata_test.go` additions

- `TestFetchPersonNames` — httptest server returns fixture with one binding (`"Jamie Oliver"`); assert map contains `"Jamie Oliver"` and length is 1
- `TestFetchPersonNames_Empty` — call with `[]string{}`; assert empty map, nil error, zero HTTP calls
- `TestFetchPersonNames_NetworkError` — HTTP 500; assert non-nil error and empty map returned

**Fixture:** inline JSON (not a file) — reuse `strings.NewReader` pattern from `TestParseWikidataJSON`.

### `merger_test.go` addition

- `TestMerge_PersonExcluded` — seed `raw_kse` with `company_name = "Jamie Oliver"`; mock `FetchPersonNames` is not needed (real Wikidata not called in tests); instead, inject via a test-only httptest override of `wikidataEndpoint` serving a fixture that returns `"Jamie Oliver"` as a person — assert "Jamie Oliver" absent from `companies` table

---

## Out of Scope

- Batching: 1595-entry VALUES clause fits in one request for the public SPARQL endpoint
- Caching person names between runs: pipeline rebuilds from scratch each run
- Filtering OpenSanctions entities by person type: OS entities rarely include person-brands; KSE-only filter is sufficient
