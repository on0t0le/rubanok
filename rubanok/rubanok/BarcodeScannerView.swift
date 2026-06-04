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

    func makeUIView(context: Context) -> UIView {
        let view = UIView(frame: .zero)
        view.backgroundColor = .black

        let status = AVCaptureDevice.authorizationStatus(for: .video)
        guard status == .authorized || status == .notDetermined else {
            let label = UILabel()
            label.text = "Camera access denied.\nEnable it in Settings → Privacy → Camera."
            label.numberOfLines = 0
            label.textAlignment = .center
            label.textColor = .white
            label.font = .preferredFont(forTextStyle: .callout)
            label.translatesAutoresizingMaskIntoConstraints = false
            view.addSubview(label)
            NSLayoutConstraint.activate([
                label.centerXAnchor.constraint(equalTo: view.centerXAnchor),
                label.centerYAnchor.constraint(equalTo: view.centerYAnchor),
                label.leadingAnchor.constraint(greaterThanOrEqualTo: view.leadingAnchor, constant: 24),
                label.trailingAnchor.constraint(lessThanOrEqualTo: view.trailingAnchor, constant: -24),
            ])
            return view
        }

        let session = AVCaptureSession()
        context.coordinator.session = session

        guard
            let device = AVCaptureDevice.default(for: .video),
            let input  = try? AVCaptureDeviceInput(device: device),
            session.canAddInput(input)
        else { return view }

        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return view }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(context.coordinator, queue: .main)
        output.metadataObjectTypes = [.ean8, .ean13, .upce, .code128]

        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        view.layer.addSublayer(preview)
        context.coordinator.previewLayer = preview

        DispatchQueue.global(qos: .userInitiated).async { session.startRunning() }
        return view
    }

    func updateUIView(_ uiView: UIView, context: Context) {
        context.coordinator.previewLayer?.frame = uiView.bounds
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
