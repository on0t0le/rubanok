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

    // MARK: - search tests

    func testSearchEmptyQueryReturnsEmpty() throws {
        try db.importCompanies([[
            "id": "mondelez", "name": "Mondelez International",
            "russia_status": "Operating", "sanctioned_ua": false,
            "brands": ["Oreo", "Milka"], "sources": ["KSE"]
        ]])
        let results = try db.search(query: "")
        XCTAssertTrue(results.isEmpty)
    }

    func testSearchByCompanyNameReturnsNilMatchedBrand() throws {
        try db.importCompanies([[
            "id": "mondelez", "name": "Mondelez International",
            "russia_status": "Operating", "sanctioned_ua": false,
            "brands": ["Oreo", "Milka"], "sources": ["KSE"]
        ]])
        let results = try db.search(query: "Mondelez")
        XCTAssertEqual(results.count, 1)
        XCTAssertEqual(results[0].companyName, "Mondelez International")
        XCTAssertNil(results[0].matchedBrand)
    }

    func testSearchByBrandNameReturnsBrandMatch() throws {
        try db.importCompanies([[
            "id": "mondelez", "name": "Mondelez International",
            "russia_status": "Exited", "sanctioned_ua": true,
            "brands": ["Oreo", "Milka"], "sources": ["KSE"]
        ]])
        let results = try db.search(query: "oreo")
        XCTAssertEqual(results.count, 1)
        XCTAssertEqual(results[0].matchedBrand, "Oreo")
        XCTAssertEqual(results[0].russiaStatus, "Exited")
        XCTAssertTrue(results[0].sanctionedUA)
    }

    func testSearchNoMatchReturnsEmpty() throws {
        try db.importCompanies([[
            "id": "mondelez", "name": "Mondelez International",
            "russia_status": "Operating", "sanctioned_ua": false,
            "brands": ["Oreo", "Milka"], "sources": ["KSE"]
        ]])
        let results = try db.search(query: "zzznomatch")
        XCTAssertTrue(results.isEmpty)
    }

    func testSearchCaseInsensitiveBrandMatch() throws {
        try db.importCompanies([[
            "id": "pg", "name": "Procter & Gamble",
            "russia_status": "Operating", "sanctioned_ua": false,
            "brands": ["Gillette", "Tide"], "sources": ["KSE"]
        ]])
        let results = try db.search(query: "GILLETTE")
        XCTAssertEqual(results.count, 1)
        XCTAssertEqual(results[0].matchedBrand, "Gillette")
    }
}
