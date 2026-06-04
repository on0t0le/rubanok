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

    func testSearchHandlesNullStatusAndBrands() throws {
        try db.importCompanies([[
            "id": "unknown-co", "name": "Unknown Corp",
            "sanctioned_ua": false, "brands": [], "sources": []
            // russia_status intentionally omitted → NULL in DB
        ]])
        let results = try db.search(query: "Unknown")
        XCTAssertEqual(results.count, 1)
        XCTAssertNil(results[0].russiaStatus)
        XCTAssertNil(results[0].matchedBrand)
        XCTAssertFalse(results[0].sanctionedUA)
    }

    // MARK: - Barcode tests

    func testImportAndLookupBarcode() throws {
        let barcodes: [[String: String]] = [
            ["code": "5000112637922", "brand": "Heineken"],
            ["code": "0012000001253", "brand": "Pepsi"]
        ]
        try db.importBarcodes(barcodes)
        XCTAssertEqual(db.lookupBarcode("5000112637922"), "Heineken")
        XCTAssertEqual(db.lookupBarcode("0012000001253"), "Pepsi")
    }

    func testLookupBarcodeNotFound() {
        XCTAssertNil(db.lookupBarcode("9999999999999"))
    }

    func testImportBarcodesSkipsInvalidEntries() throws {
        let barcodes: [[String: String]] = [
            ["code": "", "brand": "Pepsi"],      // empty code → skip
            ["code": "123", "brand": ""],         // empty brand → skip
            ["code": "456", "brand": "Valid"]     // valid
        ]
        try db.importBarcodes(barcodes)
        XCTAssertNil(db.lookupBarcode(""))
        XCTAssertEqual(db.lookupBarcode("456"), "Valid")
    }

    func testImportBarcodesReplacesExisting() throws {
        try db.importBarcodes([["code": "123", "brand": "OldBrand"]])
        try db.importBarcodes([["code": "123", "brand": "NewBrand"]])
        XCTAssertEqual(db.lookupBarcode("123"), "NewBrand")
    }

    func testImportBarcodesEmptyArraySucceeds() throws {
        XCTAssertNoThrow(try db.importBarcodes([]))
    }
}
