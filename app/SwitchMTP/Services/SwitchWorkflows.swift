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
import Foundation

/// Switch-specific bulk workflows.
///
/// These exist because the useful operations on a Switch are not "copy this
/// file" but "get all my saves off the console before I do something risky".
/// Each one resolves the storage it needs by `StorageKind` rather than asking
/// the user to find it, and none of them disturb what the user is browsing.
extension MTPManager {

    // MARK: - Storage lookup

    /// Returns the connected device's storage of a given kind, if present.
    func storage(ofKind kind: StorageKind) -> MTPStorage? {
        connectionState.device?.storages.first { $0.kind == kind }
    }

    /// Storages that can be installed to, in presentation order.
    var installTargets: [MTPStorage] {
        connectionState.device?.storages.filter { $0.capabilities.installTarget } ?? []
    }

    /// True when the connected device looks like a Switch running DBI.
    var isSwitchDevice: Bool {
        connectionState.device?.profile.isSwitch ?? false
    }

    // MARK: - Workflows

    /// Extensions DBI will accept for installation. Anything else lands as a
    /// plain file and is silently ignored by the installer.
    static let installableExtensions: Set<String> = ["nsp", "nsz", "xci", "xcz"]

    /// Backs up every save on the console into a dated folder.
    ///
    /// Saves are the one thing on a Switch that cannot be re-downloaded, so this
    /// is the highest-value operation the app offers.
    func backupSaves(to parentURL: URL) -> WorkflowOutcome {
        guard let saves = storage(ofKind: .saves) else {
            return .unavailable(String(localized: "This device does not expose a Saves storage. In DBI, enable save access before connecting."))
        }
        guard saves.capabilities.read else {
            return .unavailable(String(localized: "The Saves storage is not readable on this device."))
        }
        let folder = parentURL.appendingPathComponent(Self.datedFolderName(prefix: "Switch Saves"))
        do {
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        } catch {
            return .failed(error.localizedDescription)
        }
        download(paths: ["/"], destinationURL: folder, from: saves)
        return .started(String(localized: "Backing up saves to \(folder.lastPathComponent)."))
    }

    /// Restores previously backed-up saves to the console.
    ///
    /// This overwrites save data in place and cannot be undone, so the caller
    /// MUST confirm destructively before calling. DBI must have save write
    /// access enabled or the Saves storage reports itself read-only.
    func restoreSaves(from folderURL: URL) -> WorkflowOutcome {
        guard let saves = storage(ofKind: .saves) else {
            return .unavailable(String(localized: "This device does not expose a Saves storage. In DBI, enable save access before connecting."))
        }
        guard saves.capabilities.write else {
            return .unavailable(String(localized: "The Saves storage is read-only on this device. Enable save write access in DBI, then reconnect."))
        }
        let contents = (try? FileManager.default.contentsOfDirectory(
            at: folderURL, includingPropertiesForKeys: nil,
            options: [.skipsHiddenFiles])) ?? []
        guard !contents.isEmpty else {
            return .unavailable(String(localized: "That folder is empty — pick a folder created by Back Up Saves."))
        }
        upload(sourceURLs: contents, to: saves, destination: "/")
        return .started(String(localized: "Restoring \(contents.count) item(s) to the console's save storage."))
    }

    /// Exports the Album (screenshots and video clips).
    func exportAlbum(to parentURL: URL) -> WorkflowOutcome {
        guard let album = storage(ofKind: .album) else {
            return .unavailable(String(localized: "This device does not expose an Album storage."))
        }
        let folder = parentURL.appendingPathComponent(Self.datedFolderName(prefix: "Switch Album"))
        do {
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        } catch {
            return .failed(error.localizedDescription)
        }
        download(paths: ["/"], destinationURL: folder, from: album)
        return .started(String(localized: "Exporting Album to \(folder.lastPathComponent)."))
    }

    /// Dumps the inserted gamecard.
    ///
    /// The gamecard storage is virtual: DBI generates the dump as it is read, so
    /// this is bounded by the cartridge read speed and can run for a long time.
    func dumpGamecard(to parentURL: URL) -> WorkflowOutcome {
        guard let card = storage(ofKind: .gamecard) else {
            return .unavailable(String(localized: "No gamecard is inserted, or DBI is not exposing the Gamecard storage."))
        }
        let folder = parentURL.appendingPathComponent(Self.datedFolderName(prefix: "Switch Gamecard"))
        do {
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        } catch {
            return .failed(error.localizedDescription)
        }
        download(paths: ["/"], destinationURL: folder, from: card)
        return .started(String(localized: "Dumping gamecard to \(folder.lastPathComponent). This can take a long time; leave the gamecard inserted."))
    }

    /// Sends files to an install storage.
    ///
    /// There is no MTP-level completion event for an install: DBI starts as soon
    /// as the object lands and reports progress on the console screen only. The
    /// caller must therefore never claim the install succeeded.
    ///
    /// Everything goes through the queue, including the very first file, so that
    /// dropping more titles mid-install extends the run instead of colliding
    /// with it.
    func install(fileURLs: [URL], to storage: MTPStorage) -> WorkflowOutcome {
        enqueueInstall(fileURLs: fileURLs, to: storage)
    }

    // MARK: - Diagnostics

    /// Copies a diagnostics report to the pasteboard for bug reports.
    func copyDiagnosticsToPasteboard(completion: @escaping (Bool) -> Void) {
        fetchDiagnostics { text in
            guard let text else {
                completion(false)
                return
            }
            let pb = NSPasteboard.general
            pb.clearContents()
            pb.setString(text, forType: .string)
            completion(true)
        }
    }

    // MARK: - Helpers

    /// A sortable, collision-resistant folder name. Colons are illegal in
    /// Finder-visible names, so the time uses dashes.
    private static func datedFolderName(prefix: String) -> String {
        let fmt = DateFormatter()
        fmt.locale = Locale(identifier: "en_US_POSIX")
        fmt.dateFormat = "yyyy-MM-dd HH-mm-ss"
        return "\(prefix) \(fmt.string(from: Date()))"
    }
}

/// The result of kicking off a workflow.
///
/// Transfers are asynchronous, so `started` means "accepted and running", never
/// "finished". Callers surface the message and then follow the normal transfer
/// progress UI.
enum WorkflowOutcome {
    case started(String)
    case unavailable(String)
    case failed(String)

    var message: String {
        switch self {
        case .started(let m), .unavailable(let m), .failed(let m): return m
        }
    }

    var isError: Bool {
        if case .started = self { return false }
        return true
    }
}
