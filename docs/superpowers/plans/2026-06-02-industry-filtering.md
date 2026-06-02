# Industry Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Exclude non-consumer companies (Gazprom, Sberbank, airlines, etc.) from the pipeline output so the iOS app only surfaces brands and products purchasable in a grocery store.

**Architecture:** Three file changes — `db.go` adds `industry TEXT` to `raw_kse`; `kse.go` captures Yale HTML `cell[2]` (Industry) and stores it; `merger.go` adds `isConsumer()` filter in `loadKSE()`, dropping Financials/Energy/Industrials/etc. rows before KSE→OS matching begins. OpenSanctions entities are unaffected (they have no industry field).

**Tech Stack:** Go, SQLite (modernc driver), goquery (already imported)

---

## File Map

| File | Change |
|------|--------|
| `pipeline/internal/db/db.go` | Add `industry TEXT` to `raw_kse` CREATE TABLE |
| `pipeline/internal/db/db_test.go` | Add `TestRawKSEHasIndustryColumn` |
| `pipeline/internal/importer/kse.go` | Capture `cell[2]` in `parseYaleHTML`; add `industry` to INSERT |
| `pipeline/internal/importer/testdata/kse.html` | Add Sberbank (Financials) row; update existing row count to 6 |
| `pipeline/internal/importer/kse_test.go` | Add `TestImportKSEStoresIndustry`; update count 5→6 |
| `pipeline/internal/merger/merger.go` | Add `industry` to `kseRow`; add `consumerIndustries` + `isConsumer()`; update `loadKSE()` |
| `pipeline/internal/merger/merger_test.go` | Add `TestIsConsumer` + `TestMerge_IndustryFilter` |

---

## Task 1: Schema — add `industry` column to `raw_kse`

**Files:**
- Modify: `pipeline/internal/db/db.go`
- Modify: `pipeline/internal/db/db_test.go`

- [ ] **Step 1: Write failing test**

Add to `pipeline/internal/db/db_test.go` after `TestCreateSchemaIdempotent`:

```go
func TestRawKSEHasIndustryColumn(t *testing.T) {
	f, err := os.CreateTemp("", "db-test-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())
	conn, err := Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := CreateSchema(conn); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(`INSERT INTO raw_kse (company_name, status, industry) VALUES ('Test', 'Operating', 'Consumer Staples')`)
	if err != nil {
		t.Fatalf("industry column missing or wrong: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/db/ -run TestRawKSEHasIndustryColumn -v 2>&1 | tail -10
```

Expected: `FAIL` — `table raw_kse has no column named industry`

- [ ] **Step 3: Add `industry TEXT` to `raw_kse` schema**

In `pipeline/internal/db/db.go`, change the `raw_kse` CREATE TABLE block from:

```sql
CREATE TABLE IF NOT EXISTS raw_kse (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    company_name TEXT NOT NULL,
    status       TEXT,
    last_updated TEXT
);
```

to:

```sql
CREATE TABLE IF NOT EXISTS raw_kse (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    company_name TEXT NOT NULL,
    status       TEXT,
    industry     TEXT,
    last_updated TEXT
);
```

- [ ] **Step 4: Run all db tests**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/db/ -v 2>&1 | tail -10
```

Expected: all 3 tests pass including `TestRawKSEHasIndustryColumn`

- [ ] **Step 5: Commit**

```bash
git -C /Users/on0t0le/projects/personal/rubanok add \
  pipeline/internal/db/db.go \
  pipeline/internal/db/db_test.go
git -C /Users/on0t0le/projects/personal/rubanok commit -m "feat(db): add industry column to raw_kse schema"
```

---

## Task 2: Importer — capture and store `industry` from Yale HTML

**Files:**
- Modify: `pipeline/internal/importer/kse.go`
- Modify: `pipeline/internal/importer/testdata/kse.html`
- Modify: `pipeline/internal/importer/kse_test.go`

- [ ] **Step 1: Add Sberbank (Financials) row to test fixture**

Replace full contents of `pipeline/internal/importer/testdata/kse.html` with:

```html
<!DOCTYPE html>
<html>
<body>
<table>
<thead>
<tr><th>Name</th><th>Action</th><th>Industry</th><th>Country</th></tr>
</thead>
<tbody>
<tr><td>Mondelez International</td><td>continues operating in Russia despite criticism</td><td>Consumer Staples</td><td>USA</td></tr>
<tr><td>IKEA</td><td>fully exited Russia and closed all stores</td><td>Retail</td><td>Sweden</td></tr>
<tr><td>McDonald's</td><td>suspended operations in Russia</td><td>Restaurant</td><td>USA</td></tr>
<tr><td>Nestlé</td><td>reduced product range in Russia</td><td>Consumer Staples</td><td>Switzerland</td></tr>
<tr><td></td><td>some action</td><td>Unknown</td><td></td></tr>
<tr><td>NoMatch Corp</td><td>under review by board</td><td>Other</td><td>UK</td></tr>
<tr><td>Sberbank</td><td>continues operating in Russia</td><td>Financials</td><td>Russia</td></tr>
</tbody>
</table>
</body>
</html>
```

- [ ] **Step 2: Write failing tests**

In `pipeline/internal/importer/kse_test.go`:

Update the count in `TestImportKSEFromPath` from 5 to 6:

```go
if count != 6 {
    t.Errorf("got %d rows, want 6", count)
}
```

Add `TestImportKSEStoresIndustry` after `TestImportKSESetsLastUpdated`:

```go
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
```

- [ ] **Step 3: Run to verify they fail**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/importer/ -run "TestImportKSEFromPath|TestImportKSEStoresIndustry" -v 2>&1 | tail -15
```

Expected: `TestImportKSEFromPath` FAIL (`got 5 rows, want 6`) and `TestImportKSEStoresIndustry` FAIL (`industry = "", want Consumer Staples`)

- [ ] **Step 4: Update `parseYaleHTML` to capture `cell[2]` and include in INSERT**

In `pipeline/internal/importer/kse.go`:

Change the INSERT statement from:
```go
stmt, err := tx.Prepare(`INSERT INTO raw_kse (company_name, status, last_updated) VALUES (?, ?, ?)`)
```
to:
```go
stmt, err := tx.Prepare(`INSERT INTO raw_kse (company_name, status, industry, last_updated) VALUES (?, ?, ?, ?)`)
```

Change the row parsing block from:
```go
cells := row.Find("td")
name := strings.TrimSpace(cells.Eq(0).Text())
if name == "" {
    return
}
action := strings.TrimSpace(cells.Eq(1).Text())
status := mapYaleStatus(action)
if _, execErr := stmt.Exec(name, status, today); execErr != nil {
    insertErr = fmt.Errorf("insert %s: %w", name, execErr)
}
```
to:
```go
cells := row.Find("td")
name := strings.TrimSpace(cells.Eq(0).Text())
if name == "" {
    return
}
action := strings.TrimSpace(cells.Eq(1).Text())
status := mapYaleStatus(action)
industry := strings.TrimSpace(cells.Eq(2).Text())
if _, execErr := stmt.Exec(name, status, industry, today); execErr != nil {
    insertErr = fmt.Errorf("insert %s: %w", name, execErr)
}
```

- [ ] **Step 5: Run all importer tests**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/importer/ -v 2>&1 | tail -15
```

Expected: all 6 tests pass including `TestImportKSEStoresIndustry`

- [ ] **Step 6: Commit**

```bash
git -C /Users/on0t0le/projects/personal/rubanok add \
  pipeline/internal/importer/kse.go \
  pipeline/internal/importer/testdata/kse.html \
  pipeline/internal/importer/kse_test.go
git -C /Users/on0t0le/projects/personal/rubanok commit -m "feat(pipeline): capture industry from Yale HTML in KSE importer"
```

---

## Task 3: Merger — filter non-consumer industries

**Files:**
- Modify: `pipeline/internal/merger/merger.go`
- Modify: `pipeline/internal/merger/merger_test.go`

- [ ] **Step 1: Write failing tests**

Add to `pipeline/internal/merger/merger_test.go` before the `contains` helper function:

```go
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
```

- [ ] **Step 2: Run to verify they fail**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/merger/ -run "TestIsConsumer|TestMerge_IndustryFilter" -v 2>&1 | tail -15
```

Expected: compile error — `undefined: isConsumer`

- [ ] **Step 3: Add `industry` to `kseRow`, add `isConsumer`, update `loadKSE`**

In `pipeline/internal/merger/merger.go`:

Change `kseRow` from:
```go
type kseRow struct {
	name        string
	status      string
	lastUpdated string
}
```
to:
```go
type kseRow struct {
	name        string
	status      string
	industry    string
	lastUpdated string
}
```

Add `consumerIndustries` and `isConsumer` after the `kseRow` struct definition:

```go
var consumerIndustries = map[string]bool{
	"Consumer Staples":        true,
	"Consumer Discretionary":  true,
}

func isConsumer(industry string) bool {
	if industry == "" {
		return true
	}
	return consumerIndustries[industry]
}
```

Replace `loadKSE` entirely:

```go
func loadKSE(conn *sql.DB) ([]kseRow, error) {
	rows, err := conn.Query(`
		SELECT company_name, COALESCE(status, 'Unknown'), COALESCE(industry, ''), COALESCE(last_updated, '')
		FROM raw_kse
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []kseRow
	for rows.Next() {
		var k kseRow
		if err := rows.Scan(&k.name, &k.status, &k.industry, &k.lastUpdated); err != nil {
			return nil, err
		}
		if isConsumer(k.industry) {
			result = append(result, k)
		}
	}
	return result, rows.Err()
}
```

- [ ] **Step 4: Run all merger tests**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./internal/merger/ -v 2>&1 | tail -25
```

Expected: all tests pass including `TestIsConsumer` and `TestMerge_IndustryFilter`

- [ ] **Step 5: Run full test suite**

```bash
cd /Users/on0t0le/projects/personal/rubanok/pipeline && go test ./... 2>&1 | tail -10
```

Expected: all packages pass

- [ ] **Step 6: Commit**

```bash
git -C /Users/on0t0le/projects/personal/rubanok add \
  pipeline/internal/merger/merger.go \
  pipeline/internal/merger/merger_test.go
git -C /Users/on0t0le/projects/personal/rubanok commit -m "feat(pipeline): filter non-consumer industries from KSE data"
```
