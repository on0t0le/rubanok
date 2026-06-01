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
