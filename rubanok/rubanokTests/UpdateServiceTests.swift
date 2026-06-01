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

private func gzipCompress(_ input: Data) -> Data {
    var stream = z_stream()
    let initStatus = deflateInit2_(&stream, Z_DEFAULT_COMPRESSION, Z_DEFLATED,
                                   MAX_WBITS + 16, 8, Z_DEFAULT_STRATEGY,
                                   ZLIB_VERSION, Int32(MemoryLayout<z_stream>.size))
    precondition(initStatus == Z_OK, "deflateInit2_ failed: \(initStatus)")
    defer { deflateEnd(&stream) }

    return input.withUnsafeBytes { src in
        stream.next_in  = UnsafeMutablePointer<Bytef>(mutating: src.bindMemory(to: Bytef.self).baseAddress!)
        stream.avail_in = uInt(input.count)
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
}

// @MainActor required: UpdateService is @MainActor
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
