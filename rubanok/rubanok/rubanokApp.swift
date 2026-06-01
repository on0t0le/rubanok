//
//  rubanokApp.swift
//  rubanok
//
//  Created by Anatolii Buhrovyi on 01.06.2026.
//

import SwiftUI

@main
struct rubanokApp: App {
    @State private var updateDone = false

    var body: some Scene {
        WindowGroup {
            if updateDone {
                ContentView()
            } else {
                UpdateView(onDone: { updateDone = true })
            }
        }
    }
}
