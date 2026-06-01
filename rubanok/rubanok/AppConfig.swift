import Foundation

enum AppConfig {
    static let versionURL = URL(string: "https://on0t0le.github.io/rubanok/version.json")!
    static let datasetURL = URL(string: "https://on0t0le.github.io/rubanok/companies.json.gz")!
    static let versionKey = "stored_dataset_version"
}
