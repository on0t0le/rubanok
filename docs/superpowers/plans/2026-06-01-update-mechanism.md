# Brand Check UA: Update Mechanism Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement on-launch dataset version check and blocking update flow for the Brand Check UA iOS app, plus a Ukrainian-themed app icon.

**Architecture:** `UpdateService` orchestrates network calls (URLSession, async/await) and delegates SQLite writes to `DatabaseManager`; `UpdateView` blocks the UI until update completes; `rubanokApp` gates `ContentView` behind the update; gzip decompression via zlib C API (no external packages).

**Tech Stack:** Swift 5.9+, SwiftUI, libsqlite3 (bundled with iOS), zlib C API (bundled), URLSession, XCTest

---

## File Map

**Create:**
- `rubanok/rubanok/AppConfig.swift` — URL constants and UserDefaults keys
- `rubanok/rubanok/DatabaseManager.swift` — libsqlite3 wrapper, schema creation, batch import
- `rubanok/rubanok/UpdateService.swift` — version check, download, gunzip, import orchestration
- `rubanok/rubanok/UpdateView.swift` — blocking SwiftUI launch screen with status states
- `scripts/generate_icon.py` — draws shield+checkmark icon, outputs 1024×1024 PNG

**Modify:**
- `rubanok/rubanok/rubanokApp.swift` — gates ContentView behind UpdateView
- `rubanok/rubanok/Assets.xcassets/AppIcon.appiconset/Contents.json` — reference generated PNG

**Test files (add to `rubanokTests` target):**
- `rubanok/rubanokTests/DatabaseManagerTests.swift`
- `rubanok/rubanokTests/UpdateServiceTests.swift`

> **Xcode target requirement:** Every new `.swift` source file must be added to the Xcode build target after creation. In Xcode: right-click the `rubanok` group in the Project Navigator → **Add Files to 'rubanok'** → select file → confirm **Target: rubanok** is checked. Test files go in the `rubanokTests` target the same way.

---

## Task 1: AppConfig

**Files:**
- Create: `rubanok/rubanok/AppConfig.swift`

- [ ] **Step 1: Create AppConfig.swift**

```swift
import Foundation

enum AppConfig {
    static let versionURL = URL(string: "https://on0t0le.github.io/rubanok/version.json")!
    static let datasetURL = URL(string: "https://on0t0le.github.io/rubanok/companies.json.gz")!
    static let versionKey = "stored_dataset_version"
}
```

- [ ] **Step 2: Add AppConfig.swift to the `rubanok` target in Xcode**

- [ ] **Step 3: Commit**

```bash
git add rubanok/rubanok/AppConfig.swift
git commit -m "feat(ios): add AppConfig with dataset endpoint URLs"
```

---

## Task 2: App Icon

**Files:**
- Create: `scripts/generate_icon.py`
- Modify: `rubanok/rubanok/Assets.xcassets/AppIcon.appiconset/Contents.json`
- Generate: `rubanok/rubanok/Assets.xcassets/AppIcon.appiconset/icon_1024.png`

The project uses Xcode 15+ single-size format — one 1024×1024 PNG covers all display sizes.

- [ ] **Step 1: Install Pillow**

```bash
pip3 install Pillow
```

Expected: `Successfully installed Pillow-X.X.X` or `Requirement already satisfied`

- [ ] **Step 2: Create scripts/generate_icon.py**

```python
#!/usr/bin/env python3
"""Generate rubanok app icon: shield shape, Ukrainian blue/yellow, white checkmark."""
import os
import math
from PIL import Image, ImageDraw

BLUE   = (0, 91, 187, 255)     # #005BBB
YELLOW = (255, 215, 0, 255)    # #FFD700
WHITE  = (255, 255, 255, 255)

def shield_polygon(size: int, margin_frac: float = 0.06) -> list:
    m = int(size * margin_frac)
    x0, y0 = m, m
    w = size - 2 * m
    h = size - 2 * m
    return [
        (x0,          y0),
        (x0 + w,      y0),
        (x0 + w,      y0 + int(h * 0.62)),
        (x0 + w // 2, y0 + h),
        (x0,          y0 + int(h * 0.62)),
    ]

def shrink_polygon(points: list, amount: int) -> list:
    cx = sum(p[0] for p in points) / len(points)
    cy = sum(p[1] for p in points) / len(points)
    result = []
    for x, y in points:
        dx, dy = x - cx, y - cy
        length = math.hypot(dx, dy)
        if length > 0:
            result.append((int(x - dx / length * amount),
                           int(y - dy / length * amount)))
        else:
            result.append((int(x), int(y)))
    return result

def draw_icon(size: int) -> Image.Image:
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    border = max(3, size // 16)
    outer = shield_polygon(size)
    inner = shrink_polygon(outer, border)

    draw.polygon(outer, fill=YELLOW)
    draw.polygon(inner, fill=BLUE)

    # Checkmark: two line segments meeting at the check point
    lw = max(4, size // 10)
    s = size
    p1 = (int(s * 0.25), int(s * 0.54))  # left tip
    p2 = (int(s * 0.43), int(s * 0.72))  # bottom corner
    p3 = (int(s * 0.75), int(s * 0.34))  # right tip
    draw.line([p1, p2], fill=WHITE, width=lw)
    draw.line([p2, p3], fill=WHITE, width=lw)

    return img

CONTENTS_JSON = """{
  "images" : [
    {
      "filename" : "icon_1024.png",
      "idiom" : "universal",
      "platform" : "ios",
      "size" : "1024x1024"
    }
  ],
  "info" : {
    "author" : "xcode",
    "version" : 1
  }
}
"""

def main():
    script_dir = os.path.dirname(os.path.abspath(__file__))
    repo_root  = os.path.dirname(script_dir)
    out_dir = os.path.join(repo_root, "rubanok", "rubanok",
                           "Assets.xcassets", "AppIcon.appiconset")
    os.makedirs(out_dir, exist_ok=True)

    img = draw_icon(1024)
    png_path = os.path.join(out_dir, "icon_1024.png")
    img.save(png_path, "PNG")
    print(f"Generated {png_path}")

    contents_path = os.path.join(out_dir, "Contents.json")
    with open(contents_path, "w") as f:
        f.write(CONTENTS_JSON)
    print(f"Updated  {contents_path}")

if __name__ == "__main__":
    main()
```

- [ ] **Step 3: Run icon generator**

```bash
python3 scripts/generate_icon.py
```

Expected:
```
Generated .../AppIcon.appiconset/icon_1024.png
Updated  .../AppIcon.appiconset/Contents.json
```

- [ ] **Step 4: Verify PNG exists and is the right size**

```bash
python3 -c "
from PIL import Image
img = Image.open('rubanok/rubanok/Assets.xcassets/AppIcon.appiconset/icon_1024.png')
print(img.size)
"
```

Expected: `(1024, 1024)`

- [ ] **Step 5: Commit**

```bash
git add scripts/generate_icon.py \
        "rubanok/rubanok/Assets.xcassets/AppIcon.appiconset/icon_1024.png" \
        "rubanok/rubanok/Assets.xcassets/AppIcon.appiconset/Contents.json"
git commit -m "feat(ios): add shield+checkmark app icon in Ukrainian colors"
```

---

## Task 3: DatabaseManager

**Files:**
- Create: `rubanok/rubanok/DatabaseManager.swift`
- Create: `rubanok/rubanokTests/DatabaseManagerTests.swift`

### Step 1: Write failing tests

- [ ] **Create DatabaseManagerTests.swift**

```swift
import XCTest
import SQLite3
@testable import rubanok

final class DatabaseManagerTests: XCTestCase {
    var db: DatabaseManager!

    override func setUpWithError() throws {
        db = try DatabaseManager(path: ":memory:")
        try db.createSchema()
    }

    override func tearDownWithError() throws {
        db = nil
    }

    func testSchemaIsIdempotent() throws {
        XCTAssertNoThrow(try db.createSchema())
    }

    func testImportOneCompany() throws {
        let companies: [[String: Any]] = [[
            "id": "mondelez", "name": "Mondelez International",
            "russia_status": "Operating", "sanctioned_ua": false,
            "brands": ["Oreo", "Milka"], "sources": ["KSE"]
        ]]
        try db.importCompanies(companies)
        XCTAssertEqual(try db.companyCount(), 1)
    }

    func testImportReplacesExistingEntry() throws {
        let c: [String: Any] = [
            "id": "acme", "name": "Acme", "russia_status": "Exited",
            "sanctioned_ua": false, "brands": [], "sources": []
        ]
        try db.importCompanies([c])
        try db.importCompanies([c])
        XCTAssertEqual(try db.companyCount(), 1)
    }

    func testImportEmptyArraySucceeds() throws {
        XCTAssertNoThrow(try db.importCompanies([]))
        XCTAssertEqual(try db.companyCount(), 0)
    }
}
```

- [ ] **Add DatabaseManagerTests.swift to the `rubanokTests` target in Xcode**

- [ ] **Step 2: Run tests — verify they fail (DatabaseManager not yet defined)**

```bash
xcodebuild test \
  -project rubanok/rubanok.xcodeproj \
  -scheme rubanok \
  -destination 'platform=iOS Simulator,name=iPhone 16' \
  -only-testing:rubanokTests/DatabaseManagerTests \
  2>&1 | grep -E "error:|Build FAILED"
```

Expected: build error — `DatabaseManager` undefined.

### Step 3: Implement DatabaseManager

- [ ] **Create DatabaseManager.swift**

```swift
import Foundation
import SQLite3

enum DBError: Error {
    case openFailed(String)
    case execFailed(String)
    case prepareFailed(String)
    case stepFailed(String)
}

final class DatabaseManager {
    private var db: OpaquePointer?
    private let sqliteTransient = unsafeBitCast(-1, to: sqlite3_destructor_type.self)

    init(path: String) throws {
        guard sqlite3_open(path, &db) == SQLITE_OK else {
            throw DBError.openFailed(errMsg())
        }
    }

    deinit { sqlite3_close(db) }

    func createSchema() throws {
        let sql = """
            CREATE TABLE IF NOT EXISTS companies (
                id           TEXT PRIMARY KEY,
                name         TEXT NOT NULL,
                russia_status TEXT,
                sanctioned_ua INTEGER DEFAULT 0,
                brands_json  TEXT,
                sources_json TEXT
            );
            CREATE TABLE IF NOT EXISTS meta (
                key   TEXT PRIMARY KEY,
                value TEXT
            );
        """
        try exec(sql)
    }

    func importCompanies(_ companies: [[String: Any]]) throws {
        try exec("BEGIN TRANSACTION")
        do {
            let sql = """
                INSERT OR REPLACE INTO companies
                    (id, name, russia_status, sanctioned_ua, brands_json, sources_json)
                VALUES (?, ?, ?, ?, ?, ?)
            """
            var stmt: OpaquePointer?
            guard sqlite3_prepare_v2(db, sql, -1, &stmt, nil) == SQLITE_OK else {
                throw DBError.prepareFailed(errMsg())
            }
            defer { sqlite3_finalize(stmt) }

            for company in companies {
                sqlite3_reset(stmt)
                sqlite3_clear_bindings(stmt)

                let id         = company["id"]           as? String ?? ""
                let name       = company["name"]         as? String ?? ""
                let status     = company["russia_status"] as? String
                let sanctioned = Int32((company["sanctioned_ua"] as? Bool) == true ? 1 : 0)
                let brands     = jsonString(company["brands"])
                let sources    = jsonString(company["sources"])

                bindText(stmt, 1, id)
                bindText(stmt, 2, name)
                bindTextOrNull(stmt, 3, status)
                sqlite3_bind_int(stmt, 4, sanctioned)
                bindTextOrNull(stmt, 5, brands)
                bindTextOrNull(stmt, 6, sources)

                guard sqlite3_step(stmt) == SQLITE_DONE else {
                    throw DBError.stepFailed(errMsg())
                }
            }
            try exec("COMMIT")
        } catch {
            try? exec("ROLLBACK")
            throw error
        }
    }

    func companyCount() throws -> Int {
        var stmt: OpaquePointer?
        guard sqlite3_prepare_v2(db, "SELECT COUNT(*) FROM companies", -1, &stmt, nil) == SQLITE_OK else {
            throw DBError.prepareFailed(errMsg())
        }
        defer { sqlite3_finalize(stmt) }
        return sqlite3_step(stmt) == SQLITE_ROW ? Int(sqlite3_column_int(stmt, 0)) : 0
    }

    // MARK: - Private helpers

    private func exec(_ sql: String) throws {
        var msg: UnsafeMutablePointer<CChar>?
        guard sqlite3_exec(db, sql, nil, nil, &msg) == SQLITE_OK else {
            let text = msg.map { String(cString: $0) } ?? "unknown"
            sqlite3_free(msg)
            throw DBError.execFailed(text)
        }
    }

    private func bindText(_ stmt: OpaquePointer?, _ col: Int32, _ value: String) {
        sqlite3_bind_text(stmt, col, (value as NSString).utf8String, -1, sqliteTransient)
    }

    private func bindTextOrNull(_ stmt: OpaquePointer?, _ col: Int32, _ value: String?) {
        if let v = value { bindText(stmt, col, v) } else { sqlite3_bind_null(stmt, col) }
    }

    private func jsonString(_ value: Any?) -> String? {
        guard let v = value,
              let data = try? JSONSerialization.data(withJSONObject: v),
              let str  = String(data: data, encoding: .utf8) else { return nil }
        return str
    }

    private func errMsg() -> String {
        db.map { String(cString: sqlite3_errmsg($0)) } ?? "no db"
    }
}

// MARK: - Shared instance

extension DatabaseManager {
    static let shared: DatabaseManager = {
        let dir  = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        let path = dir.appendingPathComponent("brandcheck.sqlite").path
        guard let instance = try? DatabaseManager(path: path) else {
            fatalError("Cannot open SQLite database at \(path)")
        }
        try? instance.createSchema()
        return instance
    }()
}
```

- [ ] **Add DatabaseManager.swift to the `rubanok` target in Xcode**

- [ ] **Step 4: Run tests — verify they pass**

```bash
xcodebuild test \
  -project rubanok/rubanok.xcodeproj \
  -scheme rubanok \
  -destination 'platform=iOS Simulator,name=iPhone 16' \
  -only-testing:rubanokTests/DatabaseManagerTests \
  2>&1 | grep -E "Test Suite|FAILED|error:"
```

Expected:
```
Test Suite 'DatabaseManagerTests' passed at ...
```

- [ ] **Step 5: Commit**

```bash
git add rubanok/rubanok/DatabaseManager.swift \
        rubanok/rubanokTests/DatabaseManagerTests.swift
git commit -m "feat(ios): add DatabaseManager with SQLite schema and batch import"
```

---

## Task 4: UpdateService

**Files:**
- Create: `rubanok/rubanok/UpdateService.swift`
- Create: `rubanok/rubanokTests/UpdateServiceTests.swift`

### Step 1: Write failing tests

- [ ] **Create UpdateServiceTests.swift**

```swift
import XCTest
import zlib
@testable import rubanok

// Intercepts URLSession requests in-process during tests
final class MockURLProtocol: URLProtocol {
    static var handlers: [String: (URLRequest) throws -> (HTTPURLResponse, Data)] = [:]

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let url = request.url?.absoluteString,
              let handler = Self.handlers[url] else {
            client?.urlProtocol(self, didFailWithError: URLError(.badURL))
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}

// MARK: - Test helpers

private func mockSession() -> URLSession {
    let config = URLSessionConfiguration.ephemeral
    config.protocolClasses = [MockURLProtocol.self]
    return URLSession(configuration: config)
}

private func versionData(version: String, records: Int = 1) -> Data {
    try! JSONSerialization.data(withJSONObject: ["version": version, "records": records])
}

private func datasetGzip(companies: [[String: Any]]) -> Data {
    let json = try! JSONSerialization.data(withJSONObject: ["companies": companies])
    return gzipCompress(json)
}

// Compresses data using gzip via zlib C API (MAX_WBITS + 16 = gzip format)
private func gzipCompress(_ input: Data) -> Data {
    var stream = z_stream()
    _ = input.withUnsafeBytes { ptr -> Int32 in
        stream.next_in  = UnsafeMutablePointer<Bytef>(mutating: ptr.bindMemory(to: Bytef.self).baseAddress!)
        stream.avail_in = uInt(input.count)
        return deflateInit2_(&stream, Z_DEFAULT_COMPRESSION, Z_DEFLATED,
                             MAX_WBITS + 16, 8, Z_DEFAULT_STRATEGY,
                             ZLIB_VERSION, Int32(MemoryLayout<z_stream>.size))
    }
    defer { deflateEnd(&stream) }

    var output = Data()
    let chunk  = 65536
    var buf    = [Bytef](repeating: 0, count: chunk)
    var status = Z_OK
    repeat {
        buf.withUnsafeMutableBufferPointer { ptr in
            stream.next_out  = ptr.baseAddress!
            stream.avail_out = uInt(chunk)
        }
        status = deflate(&stream, Z_FINISH)
        output.append(contentsOf: buf.prefix(chunk - Int(stream.avail_out)))
    } while status == Z_OK
    return output
}

// MARK: - Tests
// @MainActor required: UpdateService is @MainActor, so test methods must run on main actor

@MainActor
final class UpdateServiceTests: XCTestCase {
    var db: DatabaseManager!

    override func setUpWithError() throws {
        db = try DatabaseManager(path: ":memory:")
        try db.createSchema()
        MockURLProtocol.handlers = [:]
        UserDefaults.standard.removeObject(forKey: AppConfig.versionKey)
    }

    func testUpToDateSkipsDownload() async throws {
        UserDefaults.standard.set("2026-06-01", forKey: AppConfig.versionKey)
        var downloadWasCalled = false

        MockURLProtocol.handlers[AppConfig.versionURL.absoluteString] = { _ in
            let r = HTTPURLResponse(url: AppConfig.versionURL, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (r, versionData(version: "2026-06-01"))
        }
        MockURLProtocol.handlers[AppConfig.datasetURL.absoluteString] = { _ in
            downloadWasCalled = true
            throw URLError(.unknown)
        }

        let service = UpdateService(session: mockSession(), db: db)
        let result  = await service.checkAndUpdate()

        XCTAssertEqual(result, .upToDate)
        XCTAssertFalse(downloadWasCalled)
    }

    func testDownloadsWhenVersionDiffers() async throws {
        UserDefaults.standard.set("2026-05-01", forKey: AppConfig.versionKey)

        let companies: [[String: Any]] = [[
            "id": "test", "name": "Test Co", "russia_status": "Operating",
            "sanctioned_ua": false, "brands": [], "sources": []
        ]]

        MockURLProtocol.handlers[AppConfig.versionURL.absoluteString] = { _ in
            let r = HTTPURLResponse(url: AppConfig.versionURL, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (r, versionData(version: "2026-06-01", records: 1))
        }
        MockURLProtocol.handlers[AppConfig.datasetURL.absoluteString] = { _ in
            let r = HTTPURLResponse(url: AppConfig.datasetURL, statusCode: 200, httpVersion: nil, headerFields: nil)!
            return (r, datasetGzip(companies: companies))
        }

        let service = UpdateService(session: mockSession(), db: db)
        let result  = await service.checkAndUpdate()

        XCTAssertEqual(result, .updated(records: 1))
        XCTAssertEqual(UserDefaults.standard.string(forKey: AppConfig.versionKey), "2026-06-01")
        XCTAssertEqual(try db.companyCount(), 1)
    }

    func testReturnsNetworkErrorOnVersionFetchFailure() async throws {
        MockURLProtocol.handlers[AppConfig.versionURL.absoluteString] = { _ in
            throw URLError(.notConnectedToInternet)
        }

        let service = UpdateService(session: mockSession(), db: db)
        let result  = await service.checkAndUpdate()

        guard case .networkError = result else {
            XCTFail("Expected .networkError, got \(result)")
            return
        }
    }
}
```

- [ ] **Add UpdateServiceTests.swift to the `rubanokTests` target in Xcode**

- [ ] **Step 2: Run tests — verify they fail (UpdateService not yet defined)**

```bash
xcodebuild test \
  -project rubanok/rubanok.xcodeproj \
  -scheme rubanok \
  -destination 'platform=iOS Simulator,name=iPhone 16' \
  -only-testing:rubanokTests/UpdateServiceTests \
  2>&1 | grep -E "error:|Build FAILED"
```

Expected: build error — `UpdateService` undefined.

### Step 3: Implement UpdateService

- [ ] **Create UpdateService.swift**

```swift
import Foundation
import zlib

enum UpdateResult: Equatable {
    case upToDate
    case updated(records: Int)
    case networkError(Error)
    case dataError(Error)

    static func == (lhs: UpdateResult, rhs: UpdateResult) -> Bool {
        switch (lhs, rhs) {
        case (.upToDate, .upToDate):             return true
        case (.updated(let a), .updated(let b)): return a == b
        case (.networkError, .networkError):     return true
        case (.dataError, .dataError):           return true
        default:                                 return false
        }
    }
}

enum UpdateStatus { case checking, downloading, importing }

private enum UpdateError: Error {
    case invalidResponse, decompressionFailed, invalidJSON
}

private struct VersionManifest: Decodable {
    let version: String
    let records: Int
}

@MainActor
final class UpdateService {
    private let session: URLSession
    private let db: DatabaseManager
    var onStatusChange: ((UpdateStatus) -> Void)?

    init(session: URLSession = .shared, db: DatabaseManager = .shared) {
        self.session = session
        self.db      = db
    }

    func checkAndUpdate() async -> UpdateResult {
        onStatusChange?(.checking)

        let manifest: VersionManifest
        do {
            let (data, _) = try await session.data(from: AppConfig.versionURL)
            manifest = try JSONDecoder().decode(VersionManifest.self, from: data)
        } catch {
            return .networkError(error)
        }

        let stored = UserDefaults.standard.string(forKey: AppConfig.versionKey)
        if stored == manifest.version { return .upToDate }

        onStatusChange?(.downloading)

        let compressed: Data
        do {
            let (data, response) = try await session.data(from: AppConfig.datasetURL)
            guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                return .dataError(UpdateError.invalidResponse)
            }
            compressed = data
        } catch {
            return .networkError(error)
        }

        onStatusChange?(.importing)

        do {
            let jsonData = try gunzip(compressed)
            guard let root      = try JSONSerialization.jsonObject(with: jsonData) as? [String: Any],
                  let companies = root["companies"] as? [[String: Any]] else {
                return .dataError(UpdateError.invalidJSON)
            }
            try db.importCompanies(companies)
            UserDefaults.standard.set(manifest.version, forKey: AppConfig.versionKey)
            return .updated(records: companies.count)
        } catch {
            return .dataError(error)
        }
    }

    // Gzip decompression via zlib C API (MAX_WBITS + 16 = auto-detect gzip header)
    private func gunzip(_ data: Data) throws -> Data {
        var stream = z_stream()
        let init32: Int32 = data.withUnsafeBytes { ptr in
            stream.next_in  = UnsafeMutablePointer<Bytef>(
                mutating: ptr.bindMemory(to: Bytef.self).baseAddress!)
            stream.avail_in = uInt(data.count)
            return inflateInit2_(&stream, MAX_WBITS + 16, ZLIB_VERSION,
                                 Int32(MemoryLayout<z_stream>.size))
        }
        guard init32 == Z_OK else { throw UpdateError.decompressionFailed }
        defer { inflateEnd(&stream) }

        var output    = Data()
        let chunkSize = 65536
        var buf       = [Bytef](repeating: 0, count: chunkSize)
        var status    = Z_OK

        while status != Z_STREAM_END {
            buf.withUnsafeMutableBufferPointer { ptr in
                stream.next_out  = ptr.baseAddress!
                stream.avail_out = uInt(chunkSize)
            }
            status = inflate(&stream, Z_NO_FLUSH)
            guard status != Z_STREAM_ERROR,
                  status != Z_DATA_ERROR,
                  status != Z_MEM_ERROR else {
                throw UpdateError.decompressionFailed
            }
            output.append(contentsOf: buf.prefix(chunkSize - Int(stream.avail_out)))
        }
        return output
    }
}
```

- [ ] **Add UpdateService.swift to the `rubanok` target in Xcode**

- [ ] **Step 4: Run tests — verify they pass**

```bash
xcodebuild test \
  -project rubanok/rubanok.xcodeproj \
  -scheme rubanok \
  -destination 'platform=iOS Simulator,name=iPhone 16' \
  -only-testing:rubanokTests/UpdateServiceTests \
  2>&1 | grep -E "Test Suite|FAILED|error:"
```

Expected:
```
Test Suite 'UpdateServiceTests' passed at ...
```

- [ ] **Step 5: Run all tests together**

```bash
xcodebuild test \
  -project rubanok/rubanok.xcodeproj \
  -scheme rubanok \
  -destination 'platform=iOS Simulator,name=iPhone 16' \
  2>&1 | grep -E "Test Suite 'All|FAILED|error:"
```

Expected: `Test Suite 'All tests' passed`

- [ ] **Step 6: Commit**

```bash
git add rubanok/rubanok/UpdateService.swift \
        rubanok/rubanokTests/UpdateServiceTests.swift
git commit -m "feat(ios): add UpdateService with version check, gzip download, and SQLite import"
```

---

## Task 5: UpdateView

**Files:**
- Create: `rubanok/rubanok/UpdateView.swift`

- [ ] **Step 1: Create UpdateView.swift**

```swift
import SwiftUI

struct UpdateView: View {
    let onDone: () -> Void

    @State private var phase: Phase = .checking
    @State private var errorMessage: String?
    @State private var service = UpdateService()

    var body: some View {
        ZStack {
            Color(.systemBackground).ignoresSafeArea()
            VStack(spacing: 28) {
                Image(systemName: "shield.checkered")
                    .font(.system(size: 72))
                    .foregroundStyle(.blue)

                if errorMessage == nil {
                    ProgressView()
                        .scaleEffect(1.5)
                }

                Text(phaseLabel)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)

                if let msg = errorMessage {
                    Text(msg)
                        .font(.caption)
                        .foregroundStyle(.red)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, 32)

                    Button("Retry") {
                        errorMessage = nil
                        phase = .checking
                        Task { await runUpdate() }
                    }
                    .buttonStyle(.bordered)
                }
            }
        }
        .task { await runUpdate() }
    }

    private var phaseLabel: String {
        switch phase {
        case .checking:    return "Checking for updates…"
        case .downloading: return "Downloading dataset…"
        case .importing:   return "Importing data…"
        case .done:        return "Up to date"
        }
    }

    private func runUpdate() async {
        service.onStatusChange = { status in
            switch status {
            case .checking:    phase = .checking
            case .downloading: phase = .downloading
            case .importing:   phase = .importing
            }
        }

        switch await service.checkAndUpdate() {
        case .upToDate, .updated:
            phase = .done
            try? await Task.sleep(nanoseconds: 500_000_000)
            onDone()

        case .networkError(let error):
            let hasCache = (try? DatabaseManager.shared.companyCount()) ?? 0 > 0
            if hasCache {
                errorMessage = "No internet. Using cached data."
                try? await Task.sleep(nanoseconds: 1_500_000_000)
                onDone()
            } else {
                errorMessage = "No internet connection.\nConnect to download data.\n(\(error.localizedDescription))"
            }

        case .dataError(let error):
            errorMessage = "Update failed.\n(\(error.localizedDescription))"
        }
    }

    private enum Phase { case checking, downloading, importing, done }
}

#Preview {
    UpdateView(onDone: {})
}
```

- [ ] **Add UpdateView.swift to the `rubanok` target in Xcode**

- [ ] **Step 2: Commit**

```bash
git add rubanok/rubanok/UpdateView.swift
git commit -m "feat(ios): add blocking UpdateView with status and error states"
```

---

## Task 6: Wire rubanokApp

**Files:**
- Modify: `rubanok/rubanok/rubanokApp.swift`

- [ ] **Step 1: Replace contents of rubanokApp.swift**

```swift
import SwiftUI

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

- [ ] **Step 2: Run all tests (final check)**

```bash
xcodebuild test \
  -project rubanok/rubanok.xcodeproj \
  -scheme rubanok \
  -destination 'platform=iOS Simulator,name=iPhone 16' \
  2>&1 | grep -E "Test Suite 'All|FAILED|error:"
```

Expected: `Test Suite 'All tests' passed`

- [ ] **Step 3: Build for Anatolii's iPhone (device must be connected and trusted)**

```bash
xcodebuild build \
  -project rubanok/rubanok.xcodeproj \
  -scheme rubanok \
  -destination "platform=iOS,name=Anatolii's iPhone" \
  2>&1 | tail -3
```

Expected: `** BUILD SUCCEEDED **`

- [ ] **Step 4: Commit**

```bash
git add rubanok/rubanok/rubanokApp.swift
git commit -m "feat(ios): wire UpdateView as launch gate before ContentView"
```

---

## Task 7: Manual Test Checklist (Anatolii's iPhone)

In Xcode: select **Anatolii's iPhone** as the run destination → press **Run (⌘R)**.

- [ ] **Fresh install** — no cached data, no stored version
  - Flow: "Checking…" → "Downloading…" → "Importing…" → transitions to ContentView

- [ ] **Second launch** — version matches remote
  - Flow: "Checking…" → "Up to date" → transitions quickly (≤1s)

- [ ] **Airplane mode, DB exists** (launch twice first to populate DB, then enable airplane mode, launch again)
  - Flow: "Checking…" → "No internet. Using cached data." → auto-dismisses after ~1.5s

- [ ] **Airplane mode, no DB** (delete and reinstall app while in airplane mode)
  - Flow: "Checking…" → error message with "Retry" button — stays on screen

- [ ] **Tap Retry after re-enabling wifi**
  - Flow: restarts from "Checking…", completes successfully
