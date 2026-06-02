import SwiftUI

struct ContentView: View {
    @State private var query = ""
    @State private var results: [CompanyResult] = []

    var body: some View {
        VStack(spacing: 0) {
            SearchBar(query: $query)
                .padding(.horizontal)
                .padding(.top)

            if query.isEmpty {
                emptyPrompt
            } else if results.isEmpty {
                noResults
            } else {
                List(results, id: \.companyName) {result in
                    CompanyRow(result: result)
                }
                .listStyle(.plain)
            }
        }
        .onChange(of: query) {
            results = (try? DatabaseManager.shared.search(query: query)) ?? []
        }
    }

    private var emptyPrompt: some View {
        VStack(spacing: 8) {
            Spacer()
            Image(systemName: "magnifyingglass")
                .font(.system(size: 40))
                .foregroundStyle(.tertiary)
            Text("Search brands or companies")
                .foregroundStyle(.secondary)
            Spacer()
        }
    }

    private var noResults: some View {
        VStack {
            Spacer()
            Text("No results for \"\(query)\"")
                .foregroundStyle(.secondary)
            Spacer()
        }
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

#Preview {
    ContentView()
}
