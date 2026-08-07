// SwitchMTP — a macOS MTP client for Nintendo Switch running DBI.
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

import Foundation

/// One title waiting to be installed on the console.
struct InstallQueueItem: Identifiable, Equatable {
    enum State: Equatable {
        case waiting
        case active
        /// The bytes are on the console; DBI is committing them.
        case sent
        case failed(String)
        case cancelled

        var isFinished: Bool {
            switch self {
            case .waiting, .active: return false
            case .sent, .failed, .cancelled: return true
            }
        }
    }

    let id = UUID()
    let url: URL
    let storage: MTPStorage
    var state: State = .waiting

    var name: String { url.lastPathComponent }

    /// File size on disk, or nil when it cannot be read.
    var byteSize: Int64? {
        let values = try? url.resourceValues(forKeys: [.fileSizeKey])
        return (values?.fileSize).map(Int64.init)
    }
}

extension MTPManager {

    // MARK: - Public API

    /// Adds files to the install queue, starting the first one if idle.
    ///
    /// Appending rather than rejecting is the whole point: a user who has ten
    /// titles to install should be able to drop all ten and walk away, and
    /// dropping an eleventh mid-transfer should extend the queue rather than
    /// fail with "another transfer is already running".
    @discardableResult
    func enqueueInstall(fileURLs: [URL], to storage: MTPStorage) -> WorkflowOutcome {
        let rejected = fileURLs.filter {
            !MTPManager.installableExtensions.contains($0.pathExtension.lowercased())
        }
        let accepted = fileURLs.filter {
            MTPManager.installableExtensions.contains($0.pathExtension.lowercased())
        }

        guard !accepted.isEmpty else {
            if rejected.isEmpty {
                return .unavailable(String(localized: "No files to install."))
            }
            let names = rejected.map(\.lastPathComponent).joined(separator: ", ")
            return .unavailable(String(localized: "Only .nsp, .nsz, .xci and .xcz files can be installed. Skipped: \(names)"))
        }

        // Don't queue the same file twice while it is still pending.
        let pending = Set(installQueue.filter { !$0.state.isFinished }.map(\.url))
        let fresh = accepted.filter { !pending.contains($0) }
        let duplicates = accepted.count - fresh.count

        guard !fresh.isEmpty else {
            return .unavailable(String(localized: "Those files are already queued."))
        }

        installQueue.append(contentsOf: fresh.map { InstallQueueItem(url: $0, storage: storage) })

        let started = startNextQueuedInstallIfIdle()
        if !started { scheduleInstallQueueDrain() }

        var message: String
        let waiting = installQueue.filter { $0.state == .waiting }.count
        if started && waiting == 0 {
            message = String(localized: "Installing \(fresh[0].lastPathComponent). Progress is shown on the console.")
        } else if started {
            message = String(localized: "Installing \(fresh[0].lastPathComponent); \(waiting) more queued.")
        } else {
            message = String(localized: "Queued \(fresh.count) file(s) for \(storage.name). They will install one after another.")
        }
        if !rejected.isEmpty {
            let names = rejected.map(\.lastPathComponent).joined(separator: ", ")
            message += " " + String(localized: "Skipped (not installable): \(names)")
        }
        if duplicates > 0 {
            message += " " + String(localized: "\(duplicates) already queued.")
        }
        return .started(message)
    }

    /// Removes a queued item. The item currently uploading is cancelled instead.
    func removeFromInstallQueue(_ id: UUID) {
        guard let index = installQueue.firstIndex(where: { $0.id == id }) else { return }
        if installQueue[index].state == .active {
            cancelTransfer()
            return
        }
        installQueue.remove(at: index)
    }

    /// Drops everything not yet started. The active upload keeps running; use
    /// Cancel for that, so a half-written title is never left implicit.
    func clearPendingInstalls() {
        installQueue.removeAll { $0.state == .waiting }
    }

    /// Forgets finished entries so the list does not grow without bound.
    func clearFinishedInstalls() {
        installQueue.removeAll { $0.state.isFinished }
    }

    /// Items still to be sent, including the one in flight.
    var pendingInstallCount: Int {
        installQueue.filter { !$0.state.isFinished }.count
    }

    var hasQueuedInstalls: Bool { pendingInstallCount > 0 }

    // MARK: - Queue draining

    /// Asks the queue to start its next item as soon as the device is free.
    ///
    /// The device is *not* free the instant a transfer's done callback fires:
    /// the app immediately reloads the current directory, and starting an
    /// upload while that walk is in flight would route the walk's callbacks
    /// into the upload handler. So this retries until the state machine is
    /// genuinely idle rather than assuming it already is.
    ///
    /// The retry runs for the whole of an active install, which across a 25 GB
    /// queue measured ~2,200 wake-ups. That was tempting to back off, but the
    /// `installQueueDrainScheduled` guard means a pending slow retry *swallows*
    /// the fast re-arm `finishActiveInstall` posts when a transfer ends -- so
    /// backing off to, say, five seconds buys nothing and inserts up to five
    /// seconds of dead air between every title. The wake-ups are a trivial
    /// main-queue block each and measured no CPU; the polling stays.
    func scheduleInstallQueueDrain(after delay: TimeInterval = 0.35) {
        guard !installQueueDrainScheduled else { return }
        guard installQueue.contains(where: { $0.state == .waiting }) else { return }
        installQueueDrainScheduled = true
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            guard let self else { return }
            self.installQueueDrainScheduled = false
            guard self.installQueue.contains(where: { $0.state == .waiting }) else { return }
            if !self.startNextQueuedInstallIfIdle() {
                // Still busy, or disconnected. Keep asking; a queued install
                // that silently never starts is worse than a slow one.
                if self.connectionState.isConnected {
                    self.scheduleInstallQueueDrain()
                }
            }
        }
    }

    /// Starts the next waiting item when nothing else is using the device.
    ///
    /// Returns true when an upload was dispatched.
    @discardableResult
    func startNextQueuedInstallIfIdle() -> Bool {
        guard isDeviceIdle, activeInstallItemID == nil else {
            // Deferring because an install is in flight is the normal case and
            // says nothing; only the unexplained kind is worth a line.
            if activeInstallItemID == nil {
                DebugLog.write("queue drain deferred: device busy (op not idle)")
            }
            return false
        }
        guard connectionState.isConnected else {
            DebugLog.write("queue drain deferred: not connected")
            return false
        }
        guard let index = installQueue.firstIndex(where: { $0.state == .waiting }) else { return false }

        let item = installQueue[index]
        // The storage list is rebuilt on every reconnect, so resolve by kind
        // rather than trusting a handle captured when the file was dropped.
        guard let storage = connectionState.device?.storages.first(where: { $0.id == item.storage.id })
                ?? storage(ofKind: item.storage.kind) else {
            installQueue[index].state = .failed(String(localized: "That install storage is no longer available. Reconnect the console and try again."))
            return false
        }

        installQueue[index].state = .active
        activeInstallItemID = item.id

        // The file was readable when it was queued, but that was potentially
        // many minutes and several titles ago. Failing here with a clear
        // message beats failing inside the MTP layer with a generic I/O error.
        guard FileManager.default.isReadableFile(atPath: item.url.path) else {
            installQueue[index].state = .failed(String(localized: "\(item.name) is no longer readable. It may have been moved, renamed or ejected."))
            activeInstallItemID = nil
            scheduleInstallQueueDrain()
            return false
        }

        DebugLog.write("install queue -> start \(item.name) on \(storage.name)")
        upload(sourceURLs: [item.url], to: storage, destination: "/")
        return true
    }

    /// Records the outcome of the active queue item and advances the queue.
    ///
    /// Called from the upload done callback for every upload; a no-op when the
    /// finished transfer was not a queued install.
    func finishActiveInstall(errorString: String?) {
        defer {
            // Always try to advance: a failed title should not strand the rest
            // of the queue, and the user explicitly asked for unattended
            // sequential installs.
            scheduleInstallQueueDrain()
        }

        guard let id = activeInstallItemID else { return }
        activeInstallItemID = nil
        guard let index = installQueue.firstIndex(where: { $0.id == id }) else { return }

        if let errorString {
            DebugLog.write("install queue <- \(installQueue[index].name) FAILED: \(errorString)")
            if ErrorStringLocalizer.isTransferCancelledError(errorString) {
                installQueue[index].state = .cancelled
                // A cancel is a deliberate stop. Draining the rest immediately
                // would fight the user, and the session is being reconnected
                // anyway, so park the remainder.
                for i in installQueue.indices where installQueue[i].state == .waiting {
                    installQueue[i].state = .cancelled
                }
            } else {
                installQueue[index].state = .failed(ErrorStringLocalizer.localize(errorString))
            }
        } else {
            DebugLog.write("install queue <- \(installQueue[index].name) sent OK")
            installQueue[index].state = .sent
        }
    }
}
