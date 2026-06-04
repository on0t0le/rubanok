import SwiftUI
import AVFoundation

struct BarcodeScannerView: View {
    let onScan: (String) -> Void

    var body: some View {
        CameraPreview(onScan: onScan)
            .ignoresSafeArea()
            .overlay(alignment: .top) {
                Text("Point camera at barcode")
                    .font(.caption)
                    .padding(8)
                    .background(.ultraThinMaterial)
                    .clipShape(Capsule())
                    .padding(.top, 20)
            }
    }
}

private struct CameraPreview: UIViewRepresentable {
    let onScan: (String) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onScan: onScan) }

    func makeUIView(context: Context) -> CameraContainerView {
        let view = CameraContainerView()

        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            setupSession(in: view, coordinator: context.coordinator)
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { granted in
                guard granted else { return }
                DispatchQueue.main.async {
                    self.setupSession(in: view, coordinator: context.coordinator)
                }
            }
        default:
            view.showDeniedMessage()
        }

        return view
    }

    func updateUIView(_ uiView: CameraContainerView, context: Context) {}

    private func setupSession(in view: CameraContainerView, coordinator: Coordinator) {
        let session = AVCaptureSession()
        coordinator.session = session

        guard
            let device = AVCaptureDevice.default(for: .video),
            let input  = try? AVCaptureDeviceInput(device: device),
            session.canAddInput(input)
        else { return }

        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(coordinator, queue: .main)
        output.metadataObjectTypes = [.ean8, .ean13, .upce, .code128]

        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        view.set(previewLayer: preview)
        coordinator.previewLayer = preview

        DispatchQueue.global(qos: .userInitiated).async { session.startRunning() }
    }

    final class Coordinator: NSObject, AVCaptureMetadataOutputObjectsDelegate {
        let onScan: (String) -> Void
        var session: AVCaptureSession?
        var previewLayer: AVCaptureVideoPreviewLayer?
        private var scanned = false

        init(onScan: @escaping (String) -> Void) { self.onScan = onScan }

        func metadataOutput(
            _ output: AVCaptureMetadataOutput,
            didOutput objects: [AVMetadataObject],
            from connection: AVCaptureConnection
        ) {
            guard !scanned,
                  let obj  = objects.first as? AVMetadataMachineReadableCodeObject,
                  let code = obj.stringValue
            else { return }
            scanned = true
            session?.stopRunning()
            onScan(code)
        }
    }
}

final class CameraContainerView: UIView {
    private var previewLayer: AVCaptureVideoPreviewLayer?

    override init(frame: CGRect) {
        super.init(frame: frame)
        backgroundColor = .black
    }

    required init?(coder: NSCoder) { super.init(coder: coder) }

    func set(previewLayer layer: AVCaptureVideoPreviewLayer) {
        previewLayer = layer
        self.layer.addSublayer(layer)
        layer.frame = bounds
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        previewLayer?.frame = bounds
    }

    func showDeniedMessage() {
        let label = UILabel()
        label.text = "Camera access denied.\nEnable it in Settings → Privacy → Camera."
        label.numberOfLines = 0
        label.textAlignment = .center
        label.textColor = .white
        label.font = .preferredFont(forTextStyle: .callout)
        label.translatesAutoresizingMaskIntoConstraints = false
        addSubview(label)
        NSLayoutConstraint.activate([
            label.centerXAnchor.constraint(equalTo: centerXAnchor),
            label.centerYAnchor.constraint(equalTo: centerYAnchor),
            label.leadingAnchor.constraint(greaterThanOrEqualTo: leadingAnchor, constant: 24),
            label.trailingAnchor.constraint(lessThanOrEqualTo: trailingAnchor, constant: -24),
        ])
    }
}
