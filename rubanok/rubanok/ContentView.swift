import SwiftUI

struct ContentView: View {
    @State private var query = ""
    @State private var results: [CompanyResult] = []

    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            VStack(spacing: 0) {
                SearchBar(query: $query)
                    .padding(.horizontal)
                    .padding(.top)

                if query.isEmpty {
                    ScrollView {
                        emptyPrompt
                            .frame(maxWidth: .infinity)
                            .padding(.top, 80)
                    }
                    .refreshable { await refresh() }
                } else if results.isEmpty {
                    ScrollView {
                        noResults
                            .frame(maxWidth: .infinity)
                            .padding(.top, 80)
                    }
                    .refreshable { await refresh() }
                } else {
                    List(results, id: \.companyName) { result in
                        CompanyRow(result: result)
                    }
                    .listStyle(.plain)
                    .refreshable { await refresh() }
                }
            }

            DatasetVersionLabel()
                .padding(.trailing, 12)
                .padding(.bottom, 8)
        }
        .onChange(of: query) {
            results = (try? DatabaseManager.shared.search(query: query)) ?? []
        }
    }

    private func refresh() async {
        _ = await UpdateService().checkAndUpdate()
        results = (try? DatabaseManager.shared.search(query: query)) ?? []
    }

    private var emptyPrompt: some View {
        VStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)
            Text("Search brands or companies")
                .foregroundStyle(.secondary)
        }
    }

    private var noResults: some View {
        Text("No results for \"\(query)\"")
            .foregroundStyle(.secondary)
    }
}

// MARK: - Search Bar

private struct SearchBar: View {
    @Binding var query: String

    var body: some View {
        HStack {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.secondary)
            TextField("Search brands or companies", text: $query)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
            if !query.isEmpty {
                Button {
                    query = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(.secondary)
                }
                .accessibilityLabel("Clear search")
            }
        }
        .padding(10)
        .background(Color(.secondarySystemBackground))
        .clipShape(RoundedRectangle(cornerRadius: 10))
    }
}

// MARK: - Result Row

private struct CompanyRow: View {
    let result: CompanyResult

    var body: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 2) {
                Text(result.companyName)
                    .font(.body)
                    .fontWeight(.semibold)
                if let brand = result.matchedBrand {
                    Text(brand)
                        .font(.subheadline)
                        .italic()
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 4) {
                if let status = result.russiaStatus {
                    Text(status)
                        .font(.caption)
                        .fontWeight(.medium)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(statusColor(status).opacity(0.15))
                        .foregroundStyle(statusColor(status))
                        .clipShape(Capsule())
                }
                if result.sanctionedUA {
                    Text("🇺🇦 Sanctioned")
                        .font(.caption2)
                        .foregroundStyle(.red)
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func statusColor(_ status: String) -> Color {
        switch status {
        case "Operating":           return .red
        case "Exited":              return .green
        case "Suspended",
             "Reduced Operations":  return .orange
        default:                    return .gray
        }
    }
}

// MARK: - Dataset Version Label

private struct DatasetVersionLabel: View {
    private var version: String {
        guard let raw = UserDefaults.standard.string(forKey: AppConfig.versionKey) else {
            return "no data"
        }
        // "2026-06-04T07:43:10Z" → "2026-06-04 07:43"
        let trimmed = String(raw.prefix(16))
        return trimmed.replacingOccurrences(of: "T", with: " ")
    }

    var body: some View {
        Text("Dataset: \(version)")
            .font(.caption2)
            .foregroundStyle(.tertiary)
    }
}

#Preview {
    ContentView()
}
