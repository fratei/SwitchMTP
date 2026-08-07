// SwitchMTP — a macOS MTP client for Nintendo Switch running DBI.
// Copyright (C) 2024 Neighbor_Z
// Copyright (C) 2025 fratei
//
// This program is free software; you can redistribute it and/or modify it
// under the terms of the GNU General Public License version 2 as published by
// the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
// more details.

import AppKit
import SwiftUI
import UniformTypeIdentifiers

/// The **Switch** menu: bulk workflows that are specific to a console.
///
/// These live at app level rather than in the file browser because none of them
/// operate on the current selection — they each resolve the storage they need by
/// kind and run against the whole thing.
struct SwitchCommands: Commands {
    @FocusedValue(\.mtpManager) private var manager
    @FocusedValue(\.switchMenuState) private var state

    /// Workflows move whole storages, so running two at once would interleave
    /// transfers on a single-session protocol.
    private var isBusy: Bool { state?.isTransferActive == true }
    private var isReady: Bool { state?.isConnected == true && !isBusy }

    var body: some Commands {
        CommandMenu(String(localized: "Switch")) {
            Button {
                run { m, url in m.backupSaves(to: url) }
            } label: {
                Label("Back Up Saves…", systemImage: "square.and.arrow.down.on.square")
            }
            .disabled(!isReady || !(state?.hasSaves ?? false))

            Button {
                confirmRestore()
            } label: {
                Label("Restore Saves…", systemImage: "square.and.arrow.up.on.square")
            }
            .disabled(!isReady || !(state?.canWriteSaves ?? false))

            Divider()

            Button {
                run { m, url in m.exportAlbum(to: url) }
            } label: {
                Label("Export Album…", systemImage: "photo.on.rectangle.angled")
            }
            .disabled(!isReady || !(state?.hasAlbum ?? false))

            Button {
                run { m, url in m.dumpGamecard(to: url) }
            } label: {
                Label("Dump Gamecard…", systemImage: "opticaldiscdrive")
            }
            .disabled(!isReady || !(state?.hasGamecard ?? false))

            Divider()

            installMenu

            Divider()

            Button {
                copyDiagnostics()
            } label: {
                Label("Copy Diagnostics", systemImage: "stethoscope")
            }
            // Diagnostics are most valuable when nothing is connected, so this
            // stays enabled whenever the app has actually started scanning.
            // Before that, collecting them would touch USB, so it is disabled
            // rather than failing with a misleading "try again" message.
            .disabled(state?.isStarted != true)
        }
    }

    @ViewBuilder
    private var installMenu: some View {
        let targets = state?.installTargets ?? []
        if targets.isEmpty {
            Button {} label: { Label("Install to Switch…", systemImage: "arrow.down.circle") }
                .disabled(true)
        } else {
            ForEach(targets) { target in
                Button {
                    chooseAndInstall(to: target)
                } label: {
                    Label(installTitle(for: target), systemImage: "arrow.down.circle")
                }
                .disabled(!isReady)
            }
        }
    }

    private func installTitle(for storage: MTPStorage) -> String {
        switch storage.kind {
        case .sdInstall: return String(localized: "Install to SD Card…")
        case .nandInstall: return String(localized: "Install to NAND…")
        default: return String(localized: "Install to \(storage.name)…")
        }
    }

    // MARK: - Actions

    /// Prompts for a destination folder and runs a download-style workflow.
    private func run(_ body: @escaping (MTPManager, URL) -> WorkflowOutcome) {
        guard let manager else { return }
        let panel = NSOpenPanel()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.canCreateDirectories = true
        panel.prompt = String(localized: "Choose")
        panel.message = String(localized: "Choose where to save the files.")
        guard panel.runModal() == .OK, let url = panel.url else { return }
        present(body(manager, url))
    }

    private func chooseAndInstall(to storage: MTPStorage) {
        guard let manager else { return }
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = true
        // Map our extension allow-list to UTTypes. Installable Switch formats
        // have no registered system type, so filenameExtension: is the only
        // way to build them; anything unmappable is simply skipped.
        panel.allowedContentTypes = MTPManager.installableExtensions.compactMap {
            UTType(filenameExtension: $0)
        }
        panel.prompt = String(localized: "Install")
        panel.message = String(localized: "Choose .nsp, .nsz, .xci or .xcz files to install.")
        guard panel.runModal() == .OK, !panel.urls.isEmpty else { return }
        present(manager.install(fileURLs: panel.urls, to: storage))
    }

    /// Restoring overwrites save data in place and cannot be undone, so it is
    /// gated behind an explicit destructive confirmation.
    private func confirmRestore() {
        guard let manager else { return }
        let panel = NSOpenPanel()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.prompt = String(localized: "Choose")
        panel.message = String(localized: "Choose a folder created by Back Up Saves.")
        guard panel.runModal() == .OK, let url = panel.url else { return }

        let alert = NSAlert()
        alert.alertStyle = .critical
        alert.messageText = String(localized: "Overwrite save data on the Switch?")
        alert.informativeText = String(localized: "Files in “\(url.lastPathComponent)” will replace save data on the console. This cannot be undone. Back up your current saves first if you have not already.")
        alert.addButton(withTitle: String(localized: "Restore"))
        alert.addButton(withTitle: String(localized: "Cancel"))
        guard alert.runModal() == .alertFirstButtonReturn else { return }

        present(manager.restoreSaves(from: url))
    }

    private func copyDiagnostics() {
        guard let manager else { return }
        manager.copyDiagnosticsToPasteboard { ok in
            let alert = NSAlert()
            alert.alertStyle = ok ? .informational : .warning
            alert.messageText = ok
                ? String(localized: "Diagnostics copied")
                : String(localized: "Could not collect diagnostics")
            alert.informativeText = ok
                ? String(localized: "Paste the report into a bug report. It lists USB devices, any process holding the USB interface, and the connected device's capabilities.")
                : String(localized: "Try again in a moment.")
            alert.runModal()
        }
    }

    private func present(_ outcome: WorkflowOutcome) {
        let alert = NSAlert()
        alert.alertStyle = outcome.isError ? .warning : .informational
        alert.messageText = outcome.isError
            ? String(localized: "Cannot continue")
            : String(localized: "Started")
        alert.informativeText = outcome.message
        alert.runModal()
    }
}

/// Everything the **Switch** menu needs in order to decide what is enabled.
///
/// The menu deliberately does *not* read this from `\.mtpManager`.
/// `@FocusedValue` hands back the manager but does not observe its
/// `@Published` properties, so a `Commands` body that reads
/// `manager.connectionState` is never re-evaluated when the console connects —
/// the menu freezes in whatever state it happened to be built in, leaving every
/// workflow greyed out against a live device. Publishing a plain `Equatable`
/// snapshot from a view that *does* observe the manager makes those changes
/// visible to SwiftUI. The manager itself is still injected separately, because
/// the actions need it once the user actually picks an item.
struct SwitchMenuState: Equatable {
    var isStarted = false
    var isConnected = false
    var isTransferActive = false
    var hasSaves = false
    var canWriteSaves = false
    var hasAlbum = false
    var hasGamecard = false
    var installTargets: [MTPStorage] = []

    init() {}

    init(_ manager: MTPManager) {
        isStarted = manager.isStarted
        isConnected = manager.connectionState.isConnected
        isTransferActive = manager.isTransferActive
        let saves = manager.storage(ofKind: .saves)
        hasSaves = saves != nil
        canWriteSaves = saves?.capabilities.write == true
        hasAlbum = manager.storage(ofKind: .album) != nil
        hasGamecard = manager.storage(ofKind: .gamecard) != nil
        installTargets = manager.installTargets
    }
}

struct SwitchMenuStateFocusedKey: FocusedValueKey { typealias Value = SwitchMenuState }

extension FocusedValues {
    var switchMenuState: SwitchMenuState? {
        get { self[SwitchMenuStateFocusedKey.self] }
        set { self[SwitchMenuStateFocusedKey.self] = newValue }
    }
}
