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
