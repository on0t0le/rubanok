# Design: companies.json.gz Data Pipeline

**Date:** 2026-06-01  
**Scope:** Data pipeline only — produces `companies.json.gz` and `version.json` published to GitHub Pages. iOS app is out of scope.

---

## Goal

Daily automated pipeline that aggregates company/brand data from multiple sources, resolves entities via fuzzy matching, and publishes a compressed JSON dataset to GitHub Pages for consumption by the iOS app.

---

## Architecture & Data Flow

```
OpenSanctions bulk export  ──┐
KSE CSV download           ──┤→  pipeline import  →  SQLite (raw tables)
Open Food Facts dump       ──┘                             │
data/brands.json (manual)  ──────────────────────┐         │
                                                  │         ▼
                                            pipeline merge  →  SQLite (companies table)
                                                                       │
                                                             pipeline export
                                                                       │
                                                  ┌────────────────────┤
                                                  ▼                    ▼
                                          companies.json.gz      version.json
                                                  │
                                          GitHub Pages
```

Pipeline steps run in order:
1. `pipeline import opensanctions`
2. `pipeline import kse`
3. `pipeline import brands`
4. `pipeline merge`
5. `pipeline export`

SQLite is ephemeral — created during the run, discarded after.

---

## Repository Structure

```
rubanok/
├── pipeline/                    # Go module
│   ├── cmd/pipeline/main.go     # entrypoint, subcommand routing
│   ├── internal/
│   │   ├── importer/
│   │   │   ├── opensanctions.go
│   │   │   ├── kse.go
│   │   │   └── brands.go
│   │   ├── merger/
│   │   │   └── merger.go
│   │   └── exporter/
│   │       └── exporter.go
│   ├── go.mod
│   └── go.sum
│
├── data/
│   ├── brands/
│   │   └── brands.json          # hand-curated brand→company seed (~50 entries)
│   └── overrides.json           # manual entity match fixes
│
├── output/                      # gitignored, written by pipeline in CI
│   ├── companies.json.gz
│   └── version.json
│
├── app/                         # iOS app (out of scope for this spec)
│
└── .github/
    └── workflows/
        └── update.yml
```

---

## Data Sources

### Source 1 — OpenSanctions (free tier, bulk export)

- URL: `https://data.opensanctions.org/datasets/latest/sanctions/entities.ftm.json`  
  ⚠️ Confirm before implementation: free tier gives access to specific dataset slugs (e.g. `sanctions`, `ua_nsdc`), not the full `default` bundle which may require a paid account. Check https://www.opensanctions.org/docs/bulk/ for current free tier dataset list.
- Format: FollowTheMoney (FTM) JSON lines
- Used for: UA sanctions records, company aliases, decree metadata
- Stored fields:

```sql
raw_opensanctions (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    aliases       TEXT,           -- JSON array
    country       TEXT,
    sanctioned_ua INTEGER DEFAULT 0,
    decree        TEXT,           -- e.g. "82/2025"
    sanction_date TEXT            -- ISO 8601
)
```

### Source 2 — Leave Russia / KSE (CSV download)

- URL: `https://leave-russia.org` tracker or Yale SOM public export  
  ⚠️ Confirm before implementation: verify the exact CSV download URL. Yale SOM and KSE publish the tracker jointly; the stable download link may change. Fallback: scrape the public Google Sheet they maintain.
- Format: CSV
- Used for: Russia operation status per company
- Stored fields:

```sql
raw_kse (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    company_name TEXT NOT NULL,
    status       TEXT,           -- Exited/Suspended/Reduced/Operating/Unknown
    last_updated TEXT
)
```

### Source 3 — Brands (Open Food Facts + manual)

- Open Food Facts: brands CSV export at `https://static.openfoodfacts.org/data/en.openfoodfacts.org.products.csv.gz` (filter `brands` and `owner_imported` columns)
- Manual seed: `data/brands/brands.json`
- Used for: brand→company mapping, barcode prefix data
- Stored fields:

```sql
raw_brands (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    brand_name   TEXT NOT NULL,
    company_name TEXT NOT NULL,
    source       TEXT            -- "manual" | "open_food_facts"
)
```

---

## SQLite Schema — Merged Table

```sql
CREATE TABLE companies (
    id            TEXT PRIMARY KEY,  -- slugified name e.g. "mondelez"
    name          TEXT NOT NULL,
    aliases       TEXT,              -- JSON array
    russia_status TEXT,
    sanctioned_ua INTEGER DEFAULT 0,
    decree        TEXT,
    sanction_date TEXT,
    brands        TEXT,              -- JSON array
    sources       TEXT               -- JSON array e.g. ["KSE","OpenSanctions"]
);
```

---

## Entity Matching (Fuzzy)

**Normalization steps (applied before comparison):**
1. Lowercase
2. Strip accents (`Mondelēz` → `mondelez`)
3. Remove legal suffixes: `Inc`, `LLC`, `Ltd`, `GmbH`, `Corp`, `Co`, `Group`, `Holdings`, `International`, `SA`, `AG`, `PLC`
4. Collapse whitespace, strip punctuation

**Matching thresholds:**
- Score ≥ 0.85 → auto-match
- Score 0.60–0.84 → skip (logged as "review needed", no false merges)
- Score < 0.60 → no match

**Go library:** `github.com/paul-mannino/go-fuzzywuzzy`

**Manual overrides** (`data/overrides.json`) applied before fuzzy matching. Each entry forces a KSE company name to match a specific OpenSanctions entity name, bypassing fuzzy scoring:

```json
[
  { "kse_name": "Nestle", "opensanctions_name": "Nestlé S.A." }
]
```

**Merge rules:**
- KSE only → `russia_status` set, `sanctioned_ua: false`
- OpenSanctions only → sanctions data set, `russia_status: "Unknown"`
- Both → all fields merged, `sources: ["KSE", "OpenSanctions"]`
- Brand matched to company → appended to `brands[]`

---

## Output Format

**`companies.json.gz`** — gzip-compressed JSON:

```json
{
  "companies": [
    {
      "id": "mondelez",
      "name": "Mondelēz International",
      "aliases": ["Mondelez", "Kraft Foods"],
      "russia_status": "Operating",
      "sanctioned_ua": false,
      "decree": null,
      "sanction_date": null,
      "brands": ["Oreo", "Milka", "Toblerone"],
      "sources": ["KSE", "OpenSanctions"]
    }
  ]
}
```

**`version.json`:**

```json
{
  "version": "2026-06-01",
  "records": 14321,
  "generated_at": "2026-06-01T02:15:00Z"
}
```

---

## GitHub Actions Workflow

**File:** `.github/workflows/update.yml`

```yaml
on:
  schedule:
    - cron: "0 2 * * *"
  workflow_dispatch:

jobs:
  update:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - checkout
      - setup Go 1.22
      - go build ./pipeline/cmd/pipeline
      - ./pipeline import opensanctions
      - ./pipeline import kse
      - ./pipeline import brands
      - ./pipeline merge
      - ./pipeline export
      - deploy output/ to gh-pages branch
```

**Failure behavior:** Any step failure stops the workflow. Previous `gh-pages` content is preserved unchanged — iOS app continues using last good dataset.

---

## Public Endpoints (after GitHub Pages enabled)

```
https://<org>.github.io/<repo>/companies.json.gz
https://<org>.github.io/<repo>/version.json
```

---

## Non-Goals

- iOS app (separate spec)
- Barcode scanning logic
- User accounts, backend server, database server
- AI summaries, push notifications, social features
