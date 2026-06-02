# Wikidata Person Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically exclude person-brands (e.g. Jamie Oliver) from pipeline output by detecting Wikidata Q5 (human) entities among KSE company names — replacing the manual `exclude.json` approach.

**Architecture:** New `FetchPersonNames(names []string)` in `wikidata.go` queries Wikidata SPARQL for Q5 matches among a name list. The merger calls it via a package-level `fetchPersonNames` var (swappable in tests) after `loadKSE`, builds an excludeSet from the result, and skips those companies when writing. `Merge()` signature reverts to `(conn, overrides)` — the `excludes []string` param is removed. `data/exclude.json`, `--excludes` flag, and workflow arg are all deleted.

**Tech Stack:** Go, Wikidata SPARQL (`wdt:P31 wd:Q5`), existing `wikidataClient` + `wikidataEndpoint` vars

---

## File Map

| File | Change |
|------|--------|
| `pipeline/internal/importer/wikidata.go` | Add `personQueryResponse` struct + `FetchPersonNames` function |
| `pipeline/internal/importer/wikidata_test.go` | Add 3 tests for `FetchPersonNames` |
| `pipeline/internal/merger/merger.go` | Add `fetchPersonNames` var; import importer; remove `excludes` param; call person detection after `loadKSE` |
| `pipeline/internal/merger/merger_test.go` | Revert `Merge` call sites to 2 args; add `TestMerge_PersonExcluded` |
| `pipeline/cmd/pipeline/main.go` | Remove `excludesPath`, `--excludes` flag, excludes loading; revert `Merge` call |
| `.github/workflows/update.yml` | Remove `--excludes data/exclude.json` from merge step |
| `data/exclude.json` | Delete |

---

## Task 1: `FetchPersonNames` — wikidata.go + tests

**Files:**
- Modify: `pipeline/internal/importer/wikidata.go`
- Modify: `pipeline/internal/importer/wikidata_test.go`

- [ ] **Step 1: Write failing tests**

Add to `pipeline/internal/importer/wikidata_test.go` after `TestParseWikidataJSON`:

```go
func TestFetchPersonNames(t *testing.T) {
	fixture := `{"results":{"bindings":[
		{"name":{"type":"literal","xml:lang":"en","value":"Jamie Oliver"}}
	]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		w.WriteString(fixture)
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
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/importer/ -run "TestFetchPersonNames" -v 2>&1 | tail -10
```

Expected: compile error — `undefined: FetchPersonNames`

- [ ] **Step 3: Add `personQueryResponse` struct and `FetchPersonNames` to `wikidata.go`**

Add after the `wikidataBrand` / `wikidataResponse` type block in `pipeline/internal/importer/wikidata.go` (after the closing brace of `wikidataResponse`, before `ImportBrandsFromWikidata`):

```go
type personQueryResponse struct {
	Results struct {
		Bindings []struct {
			Name struct{ Value string } `json:"name"`
		} `json:"bindings"`
	} `json:"results"`
}

// FetchPersonNames queries Wikidata and returns the subset of names that are
// instances of human (Q5). Returns empty map on empty input or error.
func FetchPersonNames(names []string) (map[string]bool, error) {
	if len(names) == 0 {
		return map[string]bool{}, nil
	}

	var sb strings.Builder
	for _, name := range names {
		sb.WriteString(`"`)
		sb.WriteString(strings.ReplaceAll(name, `"`, `\"`))
		sb.WriteString(`"@en `)
	}
	sparql := fmt.Sprintf(`SELECT DISTINCT ?name WHERE {
  VALUES ?name { %s }
  ?item rdfs:label ?name ;
        wdt:P31 wd:Q5 .
}`, sb.String())

	body := url.Values{"query": {sparql}}.Encode()
	req, err := http.NewRequest("POST", wikidataEndpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "BrandCheckUA/1.0 (https://github.com/on0t0le/rubanok)")

	resp, err := wikidataClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var pr personQueryResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&pr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	result := make(map[string]bool)
	for _, b := range pr.Results.Bindings {
		if b.Name.Value != "" {
			result[b.Name.Value] = true
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run all importer tests**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/importer/ -v 2>&1 | tail -20
```

Expected: all tests pass including 3 new `TestFetchPersonNames*` tests

- [ ] **Step 5: Commit**

```bash
git -C /Users/on0t0le/projects/personal/rubanok add \
  pipeline/internal/importer/wikidata.go \
  pipeline/internal/importer/wikidata_test.go
git -C /Users/on0t0le/projects/personal/rubanok commit -m "feat(pipeline): FetchPersonNames — detect Q5 humans via Wikidata SPARQL"
```

---

## Task 2: Merger integration

**Files:**
- Modify: `pipeline/internal/merger/merger.go`
- Modify: `pipeline/internal/merger/merger_test.go`

- [ ] **Step 1: Write failing test**

Add to `pipeline/internal/merger/merger_test.go` before the `contains` helper:

```go
func TestMerge_PersonExcluded(t *testing.T) {
	conn := tempDB(t)

	mustExec(t, conn, `INSERT INTO raw_kse (company_name, status, industry)
		VALUES ('Jamie Oliver', 'Exited', 'Consumer Staples')`)

	// Override fetchPersonNames to return Jamie Oliver as a person
	old := fetchPersonNames
	fetchPersonNames = func(names []string) (map[string]bool, error) {
		return map[string]bool{"Jamie Oliver": true}, nil
	}
	defer func() { fetchPersonNames = old }()

	if err := Merge(conn, nil); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM companies WHERE name = 'Jamie Oliver'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("Jamie Oliver (person) should be excluded, got %d rows", count)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/merger/ -run TestMerge_PersonExcluded -v 2>&1 | tail -10
```

Expected: compile error — `undefined: fetchPersonNames` and `Merge` still expects 3 args

- [ ] **Step 3: Update `merger.go`**

**3a.** Add `"pipeline/internal/importer"` to the import block in `pipeline/internal/merger/merger.go`:

```go
import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"pipeline/internal/importer"
	"pipeline/internal/normalize"
)
```

**3b.** Add the `fetchPersonNames` var after the `consumerIndustries` block (after `isConsumer` function, before `brandPair` struct):

```go
var fetchPersonNames = importer.FetchPersonNames
```

**3c.** Change `Merge` signature from:
```go
func Merge(conn *sql.DB, overrides []Override, excludes []string) error {
```
to:
```go
func Merge(conn *sql.DB, overrides []Override) error {
```

**3d.** Remove the old `excludeSet` construction block (the 4 lines that build it from `excludes`):
```go
// build exclude set: exact company name → skip
excludeSet := make(map[string]bool, len(excludes))
for _, name := range excludes {
    excludeSet[name] = true
}
```

**3e.** Add person detection right after the `brands := resolveBrands(...)` line and before the `overrideMap` block:

```go
// Detect person-brands via Wikidata and exclude them from output.
kseNames := make([]string, len(kseList))
for i, k := range kseList {
    kseNames[i] = k.name
}
excludeSet, err := fetchPersonNames(kseNames)
if err != nil {
    fmt.Printf("WARN: person detection failed: %v — no persons excluded\n", err)
    excludeSet = map[string]bool{}
}
```

- [ ] **Step 4: Update `merger_test.go` call sites**

Change all `Merge(conn, nil, nil)` → `Merge(conn, nil)` and `Merge(conn, overrides, nil)` → `Merge(conn, overrides)`:

```bash
sed -i '' 's/Merge(conn, nil, nil)/Merge(conn, nil)/g; s/Merge(conn, overrides, nil)/Merge(conn, overrides)/g' /Users/on0t0le/projects/personal/rubanok/pipeline/internal/merger/merger_test.go
```

- [ ] **Step 5: Run all merger tests**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/merger/ -v 2>&1 | tail -25
```

Expected: all tests pass including `TestMerge_PersonExcluded`

- [ ] **Step 6: Run full test suite**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./... 2>&1 | tail -10
```

Expected: all packages pass

- [ ] **Step 7: Commit**

```bash
git -C /Users/on0t0le/projects/personal/rubanok add \
  pipeline/internal/merger/merger.go \
  pipeline/internal/merger/merger_test.go
git -C /Users/on0t0le/projects/personal/rubanok commit -m "feat(pipeline): exclude Wikidata Q5 persons in merger; remove manual excludes param"
```

---

## Task 3: Cleanup — delete exclude.json, update main.go and workflow

**Files:**
- Delete: `data/exclude.json`
- Modify: `pipeline/cmd/pipeline/main.go`
- Modify: `.github/workflows/update.yml`

- [ ] **Step 1: Delete `data/exclude.json`**

```bash
git -C /Users/on0t0le/projects/personal/rubanok rm data/exclude.json
```

- [ ] **Step 2: Update `newMergeCmd` in `main.go`**

Replace the `newMergeCmd` function body in `pipeline/cmd/pipeline/main.go` from:

```go
func newMergeCmd() *cobra.Command {
	var overridesPath, excludesPath string
	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge raw tables into companies table using fuzzy matching",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := openDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			var overrides []merger.Override
			data, err := os.ReadFile(overridesPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read overrides %s: %w", overridesPath, err)
			}
			if err == nil {
				if err := json.Unmarshal(data, &overrides); err != nil {
					return fmt.Errorf("parse overrides %s: %w", overridesPath, err)
				}
			}

			var excludes []string
			edata, err := os.ReadFile(excludesPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read excludes %s: %w", excludesPath, err)
			}
			if err == nil {
				if err := json.Unmarshal(edata, &excludes); err != nil {
					return fmt.Errorf("parse excludes %s: %w", excludesPath, err)
				}
			}

			fmt.Printf("merging with %d overrides, %d excludes...\n", len(overrides), len(excludes))
			return merger.Merge(conn, overrides, excludes)
		},
	}
	cmd.Flags().StringVar(&overridesPath, "overrides", "data/overrides.json", "path to overrides JSON file")
	cmd.Flags().StringVar(&excludesPath, "excludes", "data/exclude.json", "path to excludes JSON file")
	return cmd
}
```

to:

```go
func newMergeCmd() *cobra.Command {
	var overridesPath string
	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge raw tables into companies table using fuzzy matching",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := openDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			var overrides []merger.Override
			data, err := os.ReadFile(overridesPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read overrides %s: %w", overridesPath, err)
			}
			if err == nil {
				if err := json.Unmarshal(data, &overrides); err != nil {
					return fmt.Errorf("parse overrides %s: %w", overridesPath, err)
				}
			}

			fmt.Printf("merging with %d overrides...\n", len(overrides))
			return merger.Merge(conn, overrides)
		},
	}
	cmd.Flags().StringVar(&overridesPath, "overrides", "data/overrides.json", "path to overrides JSON file")
	return cmd
}
```

- [ ] **Step 3: Update workflow**

In `.github/workflows/update.yml`, change the merge step from:

```yaml
      - name: Merge entities
        run: ./pipeline-bin merge --overrides data/overrides.json --excludes data/exclude.json
```

to:

```yaml
      - name: Merge entities
        run: ./pipeline-bin merge --overrides data/overrides.json
```

- [ ] **Step 4: Run full test suite**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./... 2>&1 | tail -10
```

Expected: all packages pass

- [ ] **Step 5: Commit**

```bash
git -C /Users/on0t0le/projects/personal/rubanok add \
  pipeline/cmd/pipeline/main.go \
  .github/workflows/update.yml
git -C /Users/on0t0le/projects/personal/rubanok commit -m "chore(pipeline): remove manual exclude.json; clean up merge command"
```
