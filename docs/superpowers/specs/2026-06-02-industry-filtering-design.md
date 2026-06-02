# Industry Filtering Design

**Date:** 2026-06-02
**Scope:** Filter non-consumer companies (Gazprom, banks, airlines) from search results so only brands/products purchasable in a grocery store appear.
**Goal:** Searching the app never surfaces energy companies, financials, or industrials — only Consumer Staples and Consumer Discretionary companies pass through the pipeline.

---

## Architecture

Three files change, no new files, no new packages.

| File | Change |
|------|--------|
| `pipeline/internal/db/db.go` | Add `industry TEXT` column to `raw_kse` CREATE TABLE |
| `pipeline/internal/importer/kse.go` | Capture `cell[2]` (Industry) in `parseYaleHTML`, store in INSERT |
| `pipeline/internal/merger/merger.go` | Add `industry` field to `kseRow`; `loadKSE` filters non-consumer rows in Go |

No schema migration needed — pipeline rebuilds SQLite from scratch on every CI run.

---

## Data Flow

### Importer (`kse.go`)

`parseYaleHTML` reads Yale HTML table cells per row:
- `cell[0]` → `name`
- `cell[1]` → `action` → `mapYaleStatus(action)` → `status`
- `cell[2]` → `industry` (new)

INSERT changes from `(company_name, status, last_updated)` to `(company_name, status, industry, last_updated)`.

If `cell[2]` is missing (table has fewer columns), `industry` defaults to `""`.

### Merger (`merger.go`)

`kseRow` gains `industry string` field.

`loadKSE` scans all rows, applies `isConsumer(industry)` filter in Go before returning:

```go
var consumerIndustries = map[string]bool{
    "Consumer Staples":       true,
    "Consumer Discretionary": true,
}

func isConsumer(industry string) bool {
    if industry == "" {
        return true // non-KSE entities have no industry; always keep
    }
    return consumerIndustries[industry]
}
```

Rows where `isConsumer` returns false are silently dropped — they never reach the KSE→OS matching step and never appear in `companies`.

OpenSanctions entities are unaffected: they have no industry field and all pass through unchanged.

---

## Industry Allowlist

Yale uses GICS sector labels. Two sectors pass through:

| Sector | Examples |
|--------|---------|
| Consumer Staples | Food, beverages, household products, personal care (Nestlé, P&G, Unilever) |
| Consumer Discretionary | Clothing, restaurants, retail (McDonald's, H&M, IKEA) |

Excluded sectors (non-exhaustive): Financials, Energy, Industrials, Materials, Information Technology, Communication Services, Utilities, Real Estate.

Empty/NULL industry → kept (permissive default, safe for future data sources).

---

## Error Handling

No new failure modes introduced. `industry` is nullable in `raw_kse`; missing cell[2] silently defaults to `""` (keep). Yale table structure has 4 columns for all 5 tables — column 3 is always present.

Non-matching industry values (typos, future Yale label changes) are treated as non-consumer and dropped. If this causes unexpected data loss, the CI WARN logs will show the count delta.

---

## Testing

### `kse_test.go`
- Update `testdata/kse.html` fixture: add `Industry` column with at least one `Consumer Staples` row and one `Financials` row.
- Assert only the Consumer Staples row is inserted into `raw_kse`.

### `merger_test.go`
- `TestMerge_IndustryFilter`: seed `raw_kse` with a Financials company (`industry = "Financials"`), run `Merge`, assert company does NOT appear in `companies` table.

### Unit tests (in `merger_test.go` or new `merger_filter_test.go`)
- `TestIsConsumer`: `""` → true, `"Consumer Staples"` → true, `"Consumer Discretionary"` → true, `"Energy"` → false, `"Financials"` → false.

---

## Out of Scope

- Exposing `industry` in the `companies` table or iOS UI — no industry chip on `CompanyRow` for MVP.
- Allowlist configurability at runtime — hardcoded map is sufficient; change requires code edit + CI run.
- Health Care sector — excluded for MVP (not typical grocery store products; can add later).
- OpenSanctions industry tagging — OS entities have no industry field; filtering them requires a separate data source.
