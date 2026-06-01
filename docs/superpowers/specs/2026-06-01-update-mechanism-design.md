# Update Mechanism — Design Spec
**Date:** 2026-06-01
**Scope:** iOS app launch-time dataset update + app icon

---

## Goal

On every launch, check if a newer dataset exists on GitHub Pages. If newer, download, decompress, and replace local SQLite database. Block app UI until update completes (or fails gracefully).

---

## Endpoints

| Resource | URL |
|---|---|
| Version manifest | `https://on0t0le.github.io/rubanok/version.json` |
| Dataset | `https://on0t0le.github.io/rubanok/companies.json.gz` |

`version.json` shape:
```json
{ "version": "2026-06-01", "records": 14321 }
```

---

## Architecture

5 files, zero external packages. Uses `libsqlite3` (bundled with iOS) and `Compression` framework (bundled with iOS).

```
rubanok/
├── AppConfig.swift          # URL constants, UserDefaults keys
├── DatabaseManager.swift    # libsqlite3 wrapper, schema, company import
├── UpdateService.swift      # version check, download, decompress, import
├── UpdateView.swift         # blocking launch screen: spinner + status text
└── rubanokApp.swift         # (modified) shows UpdateView first, then ContentView
```

---

## Components

### AppConfig.swift
Constants only. No logic.
- `versionURL`: `https://on0t0le.github.io/rubanok/version.json`
- `datasetURL`: `https://on0t0le.github.io/rubanok/companies.json.gz`
- `versionKey`: UserDefaults key for stored version string

### DatabaseManager.swift
Singleton. Wraps `libsqlite3`.

Responsibilities:
- Open/create SQLite file in app's Application Support directory
- Create schema on first run
- `importCompanies(_ companies: [[String: Any]])` — batch insert with `INSERT OR REPLACE`

Schema:
```sql
CREATE TABLE IF NOT EXISTS companies (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  russia_status TEXT,
  sanctioned_ua INTEGER DEFAULT 0,
  brands_json TEXT,
  sources_json TEXT
);

CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT
);
```

### UpdateService.swift
`actor` or `@MainActor class`. Async/await throughout.

Steps in `checkAndUpdate()`:
1. Fetch `version.json` via `URLSession`
2. Parse version string
3. Compare with `UserDefaults.standard.string(forKey: AppConfig.versionKey)`
4. If equal → return `.upToDate`
5. If different (or nil) → download `companies.json.gz`
6. Decompress using `Compression` framework (`compression_decode_buffer`, algorithm `.zlib` / raw gzip via `NSData` with `gunzipInflate`)
7. Parse JSON → extract `companies` array
8. Call `DatabaseManager.importCompanies(_:)`
9. Save new version to `UserDefaults`
10. Return `.updated(records: Int)`

Error cases:
- Network unavailable → `.networkError(Error)` — show "No internet. Using cached data." if DB exists, else show "Connect to internet to download data."
- Version fetch fails → same as network unavailable
- Download/decompress/parse fails → `.dataError(Error)` — show error, keep existing DB

### UpdateView.swift
SwiftUI view. Shown full-screen while update runs.

States:
- `.checking` — "Checking for updates…" + `ProgressView()` (spinner)
- `.downloading` — "Downloading dataset…" + `ProgressView()` (spinner)
- `.importing` — "Importing data…" + `ProgressView()` (spinner)
- `.upToDate` — briefly shows "Up to date" then auto-dismisses (0.5s delay)
- `.error(String)` — shows error message + "Continue with cached data" button (only if DB exists) or "Retry" button
- `.noData` — "No data available. Connect to internet." + Retry button

Dismissal: sets `@State isUpdateDone = true` in parent, which triggers transition to `ContentView`.

### rubanokApp.swift
```swift
@main
struct rubanokApp: App {
    @State private var updateDone = false

    var body: some Scene {
        WindowGroup {
            if updateDone {
                ContentView()
            } else {
                UpdateView(onDone: { updateDone = true })
            }
        }
    }
}
```

---

## Data Flow

```
App Launch
    ↓
UpdateView appears (blocking)
    ↓
UpdateService.checkAndUpdate()
    ├─ Fetch version.json
    │     ├─ Fail → show error / continue with cache
    │     └─ Parse version
    │           ├─ Same as stored → .upToDate → dismiss
    │           └─ Different
    │                 ↓
    │             Download companies.json.gz
    │                 ↓
    │             Decompress (Compression framework)
    │                 ↓
    │             Parse JSON
    │                 ↓
    │             DatabaseManager.importCompanies()
    │                 ↓
    │             Save version to UserDefaults
    │                 ↓
    │             .updated → dismiss
    └─ Any error → show error state
```

---

## App Icon

Shape: shield  
Primary fill: Ukrainian blue `#005BBB`  
Accent border: Ukrainian yellow `#FFD700`  
Foreground: white checkmark  
Style: flat, no gradients  

Generated as PNG. Stored in `rubanok/Assets.xcassets/AppIcon.appiconset/`.

Required sizes (all square):
| Filename | Size |
|---|---|
| `icon_1024.png` | 1024×1024 |
| `icon_180.png` | 180×180 (60pt @3x) |
| `icon_120.png` | 120×120 (60pt @2x) |
| `icon_87.png` | 87×87 (29pt @3x) |
| `icon_80.png` | 80×80 (40pt @2x) |
| `icon_60.png` | 60×60 |
| `icon_58.png` | 58×58 (29pt @2x) |
| `icon_40.png` | 40×40 |
| `icon_29.png` | 29×29 |

---

## Testing

Device: Anatolii's iPhone (physical device, selected in Xcode scheme).

Manual test cases:
1. **Fresh install** — no DB, no stored version → downloads, imports, dismisses
2. **Up to date** — stored version matches remote → skips download, dismisses quickly
3. **New version** — stored version older → downloads new data, replaces DB
4. **No internet** — airplane mode, DB exists → error state with "Continue" button
5. **No internet, no DB** — airplane mode, fresh install → "No data" state with Retry

No UI test target automation for MVP. Run manually on device.

---

## Non-Goals (this iteration)

- Search UI
- Barcode scanner
- Company detail view
- Background refresh
- Progress percentage (indeterminate spinner only)
