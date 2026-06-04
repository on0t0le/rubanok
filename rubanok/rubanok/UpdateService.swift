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
            if let barcodes = root["barcodes"] as? [[String: String]] {
                try db.importBarcodes(barcodes)
            }
            UserDefaults.standard.set(manifest.version, forKey: AppConfig.versionKey)
            return .updated(records: companies.count)
        } catch {
            return .dataError(error)
        }
    }

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
