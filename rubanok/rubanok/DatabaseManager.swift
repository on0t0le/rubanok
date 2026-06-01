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
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: nil)
        let path = dir.appendingPathComponent("brandcheck.sqlite").path
        guard let instance = try? DatabaseManager(path: path) else {
            fatalError("Cannot open SQLite database at \(path)")
        }
        try? instance.createSchema()
        return instance
    }()
}
