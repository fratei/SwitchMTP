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

import Foundation
import Combine
import AppKit
import UserNotifications

/// Manages MTP device communication.
final class MTPManager: ObservableObject {
    
    // MARK: – Published State
    @Published var connectionState: ConnectionState = .disconnected
    @Published var files: [MTPFile] = []
    @Published var navigationStack: [String] = []
    @Published var selectedStorage: MTPStorage? = nil
    @Published var isLoading: Bool = false
    @Published var transferProgress: Double? = nil
    @Published var transferStats: TransferStatistics? = nil
    @Published var silentTransferStats: TransferStatistics? = nil
    @Published var errorMessage: String? = nil
    @Published var availableDevices: [MTPDeviceInfo] = []
    @Published private(set) var isTransferActive: Bool = false
    @Published var isShowingNameConflictAlert: Bool = false

    /// Files waiting to be sent to an install storage, plus the one in flight.
    ///
    /// DBI installs strictly one title at a time — sending a second while the
    /// console is still committing the first is the reliable way to wedge it —
    /// so this is a real queue, drained one item per `Upload` call.
    /// Mutate it through the queue API in `InstallQueue.swift`, never directly.
    @Published var installQueue: [InstallQueueItem] = []

    /// Timestamp of the most recent progress payload, used to notice stalls.
    @Published private(set) var lastProgressAt: Date? = nil

    /// Throttles progress logging: the callback fires far too often to log every
    /// one, but a transfer that stalls needs a timeline showing where it stopped.
    private var lastProgressLogAt: Date? = nil

    /// The queue entry currently being uploaded, if the running transfer is one.
    var activeInstallItemID: UUID? = nil

    /// Set while a queue-drain retry is already pending, so repeated triggers
    /// do not pile up timers.
    var installQueueDrainScheduled: Bool = false

    /// True when no FFI operation owns the device, so a queued install may start.
    ///
    /// Covers both slots. `operation` is single-slot, so starting an upload
    /// while a directory reload is in flight would route the walk's callbacks
    /// into the upload handler; and `transferKind` guards against starting a
    /// second transfer on top of one already running.
    var isDeviceIdle: Bool {
        operation == .none && transferKind == nil && !isTransferActive
    }

    /// True while a copy owns the USB session, whoever started it.
    ///
    /// Every entry point that begins a transfer must check this. MTP is a single
    /// session, so a second transfer cannot run alongside the first; it would
    /// overwrite `transferKind`, and whichever finished last would find an empty
    /// slot and be discarded, leaving `isTransferActive` true for the life of
    /// the process and the install queue jammed.
    var isTransferInFlight: Bool {
        transferKind != nil || isTransferActive
    }
    
    // NEW: Pass "" to let Go connect to the first available device, or specific ID for exact matches.
    var deviceId: String = ""
    
    var currentPath: String { navigationStack.last ?? "/" }
    var canGoBack: Bool { navigationStack.count > 1 }
    
    var sortedFiles: [MTPFile] {
        files.sorted {
            if $0.isDirectory != $1.isDirectory { return $0.isDirectory && !$1.isDirectory }
            return $0.name.localizedStandardCompare($1.name) == .orderedAscending
        }
    }

    func hasConflictingItems(for sourceURLs: [URL]) -> Bool {
        guard !sourceURLs.isEmpty else { return false }
        let existingNames = Set(files.map { $0.name })
        for url in sourceURLs {
            if existingNames.contains(url.lastPathComponent) {
                return true
            }
        }
        return false
    }
    
    // MARK: – Nxmtp routing (C callbacks cannot capture Swift context)
    private enum Operation {
        case none
        case initializing
        case fetchingStorages
        case walking
        case deleting
        case makingDirectory
        case renaming
        case disposing
    }

    /// Transfers deliberately do not live in `operation`.
    ///
    /// `operation` is a single slot, which is fine for the short operations
    /// that own the device for a moment. A transfer is different: it runs for
    /// minutes and the user is expected to keep using the app while it does.
    /// Navigating armed a walk, the walk overwrote the transfer's claim, and
    /// every subsequent progress callback was discarded -- the bar froze part
    /// way while the copy carried on, and the transfer's own completion was
    /// then misrouted into the walk handler, so it never finished on screen.
    ///
    /// Device enumeration, cancel and diagnostics already sidestep `operation`
    /// for the same reason. Transfers get the same treatment: their own slot,
    /// and their own done callback so the two can never be confused.
    private enum TransferKind {
        case uploading
        case downloading
        case silentDownloading
    }

    private var operation: Operation = .none
    private var transferKind: TransferKind?
    private var transferCompletion: ((Error?) -> Void)?
    private var scopedSourceURLs: [URL] = []
    private var scopedDestinationURL: URL?
    private var cachedDeviceInfo: (name: String, manufacturer: String)? = nil
    private var cachedProfile: DeviceProfile = .generic
    private var cachedAdvice: String = ""
    private var pendingDeviceId: String? = nil
    /// A folder the user asked for that could not be listed yet, replayed once
    /// the device is free. Set either by a reconnect (restore the old folder)
    /// or by navigating during a transfer -- MTP is a single session, so a walk
    /// issued mid-transfer would block on the Go client mutex until the copy
    /// finished, freezing the UI on "Loading…" for the whole transfer.
    @Published var pendingNavigationPath: String? = nil
    /// Directory listings from the last successful walk, keyed by storage and
    /// path. The console cannot answer a listing request while it is copying,
    /// and an install queue can run for hours, so a folder the user has already
    /// visited is shown from here immediately rather than leaving the browser
    /// dead for the whole run. It is replaced by a real listing the moment the
    /// device is free.
    private var listingCache: [String: [MTPFile]] = [:]
    /// The path the in-flight walk was asked for, so its result can be cached
    /// against the right folder.
    private var walkingPath: String? = nil
    /// True while `files` came from `listingCache` rather than the console.
    @Published var showingCachedListing: Bool = false
    /// Folder an in-flight upload is writing into. Its cached listing is
    /// discarded on completion, since the new files would otherwise be missing
    /// from it the next time it is shown from cache.
    private var uploadDestinationCacheKey: String? = nil
    private var deviceScanWorkItem: DispatchWorkItem?
    
    // MARK: – Promise Downloads (for multi-select drag-drop)
    private let promiseQueueLock = DispatchQueue(label: "app.switchmtp.promise-queue", attributes: [])
    private var promiseDownloadBatch: [(file: MTPFile, destination: URL, completion: (Error?) -> Void)] = []
    private var promiseBatchTimer: Timer?
    private var isProcessingPromiseBatch: Bool = false
    
    // MARK: – USB Hotplug & Retry Logic
    private var usbMonitor: USBMonitor?
    private var hotplugRetryCount: Int = 0
    private var retryTimer: Timer?
    private var shouldIgnoreUSBEvents: Bool = false

    /// Guards against an auto-connect loop. A device that is present but
    /// cannot be opened would otherwise be retried on every scan forever,
    /// and every failed attempt re-enumerates the port, which produces the
    /// hotplug events that trigger the next scan.
    private var autoConnectFailureDeviceId: String = ""
    private var autoConnectFailureCount: Int = 0
    private let maxAutoConnectAttempts: Int = 3

    /// Whether the device list handler may auto-connect to `id` right now.
    private func mayAutoConnect(to id: String) -> Bool {
        if id != autoConnectFailureDeviceId {
            autoConnectFailureDeviceId = id
            autoConnectFailureCount = 0
        }
        return autoConnectFailureCount < maxAutoConnectAttempts
    }

    /// Records the outcome of an auto-connect attempt so the guard above can
    /// give up on a device that never succeeds, and forgive one that does.
    func noteAutoConnectOutcome(deviceId id: String, succeeded: Bool) {
        guard !id.isEmpty else { return }
        if succeeded {
            if id == autoConnectFailureDeviceId {
                autoConnectFailureCount = 0
            }
            return
        }
        if id == autoConnectFailureDeviceId {
            autoConnectFailureCount += 1
        } else {
            autoConnectFailureDeviceId = id
            autoConnectFailureCount = 1
        }
    }
    
    
    private enum CallbackRouter {
        static weak var manager: MTPManager?
        
        // These callbacks do not capture any local context.
        static let done: NxmtpOnCbResult = { jsonPtr in
            CallbackRouter.manager?.handleDone(jsonPtr: jsonPtr)
        }
        
        // Separate callback for device list - completely independent of operation state.
        static let deviceListDone: NxmtpOnCbResult = { jsonPtr in
            CallbackRouter.manager?.handleDeviceListDone(jsonPtr: jsonPtr)
        }
        
        static let preprocess: NxmtpOnCbResult = { jsonPtr in
            CallbackRouter.manager?.handlePreprocess(jsonPtr: jsonPtr)
        }
        
        static let progress: NxmtpOnCbResult = { jsonPtr in
            CallbackRouter.manager?.handleProgress(jsonPtr: jsonPtr)
        }
        
        static let cancelDone: NxmtpOnCbResult = { jsonPtr in
            CallbackRouter.manager?.handleCancelDone(jsonPtr: jsonPtr)
        }
        
        // Diagnostics deliberately bypass `operation`: the report is most needed
        // when nothing is connected or an operation is stuck.
        static let diagnosticsDone: NxmtpOnCbResult = { jsonPtr in
            CallbackRouter.manager?.handleDiagnosticsDone(jsonPtr: jsonPtr)
        }

        // Transfers bypass `operation` too, so that a walk armed by navigating
        // mid-transfer cannot swallow the transfer's completion.
        static let transferDone: NxmtpOnCbResult = { jsonPtr in
            CallbackRouter.manager?.handleTransferDone(jsonPtr: jsonPtr)
        }
    }
    
    /// Gates every USB scan until the user has seen the first-run disclosure.
    ///
    /// Enumerating devices is not passive: to read a candidate's identity we
    /// open it, and opening it resets the USB port to take the interface back
    /// from macOS's ptpcamerad. Scanning before the user has been told that
    /// would mean disclosing the behaviour only after it had already happened.
    /// False until the first-run USB disclosure has been acknowledged. Published so
    /// the toolbar and menus can disable USB actions rather than silently ignore them.
    @Published private(set) var isStarted = false

    init() {
        CallbackRouter.manager = self

        // Initialize and start USB monitor. Monitoring itself is passive --
        // it only listens for attach/detach notifications -- but the scans it
        // triggers are not, so those are gated on `isStarted`.
        self.usbMonitor = USBMonitor { [weak self] attached in
            self?.handleUSBEvent(attached: attached)
        }
        self.usbMonitor?.startMonitoring()
    }

    /// Begins scanning for devices. Called once the first-run USB disclosure
    /// has been acknowledged.
    func start() {
        guard !isStarted else { return }
        isStarted = true
        fetchAvailableDevices()
    }
    
    deinit {
        // Clean up USB monitor and retry timer
        retryTimer?.invalidate()
        retryTimer = nil
        usbMonitor?.stopMonitoring()
        usbMonitor = nil
    }
    
    // MARK: – Diagnostics
    private var diagnosticsCompletion: ((String?) -> Void)?
    private let diagnosticsLock = NSLock()
    
    /// Fetches the backend troubleshooting report (USB enumeration, processes
    /// holding the PTP interface, device info) as pretty-printed JSON.
    func fetchDiagnostics(completion: @escaping (String?) -> Void) {
        // Diagnostics enumerate (and therefore open) USB devices, so this is
        // gated too: it is reachable from the menu bar while the first-run
        // disclosure is still on screen.
        guard isStarted else {
            completion(nil)
            return
        }
        diagnosticsLock.lock()
        if diagnosticsCompletion != nil {
            diagnosticsLock.unlock()
            completion(nil)   // one at a time; the report is cheap to retry
            return
        }
        diagnosticsCompletion = completion
        diagnosticsLock.unlock()
        
        DispatchQueue.global(qos: .userInitiated).async {
            NxmtpFetchDiagnostics(CallbackRouter.diagnosticsDone)
        }
    }
    
    private func handleDiagnosticsDone(jsonPtr: UnsafeMutablePointer<CChar>?) {
        diagnosticsLock.lock()
        let completion = diagnosticsCompletion
        diagnosticsCompletion = nil
        diagnosticsLock.unlock()
        guard let completion else { return }
        
        guard let jsonPtr else {
            DispatchQueue.main.async { completion(nil) }
            return
        }
        let raw = String(cString: jsonPtr)
        // Re-encode so the report is readable when pasted into a bug report.
        var text = raw
        if let data = raw.data(using: .utf8),
           let obj = try? JSONSerialization.jsonObject(with: data),
           let pretty = try? JSONSerialization.data(withJSONObject: obj, options: [.prettyPrinted, .sortedKeys]),
           let s = String(data: pretty, encoding: .utf8) {
            text = s
        }
        DispatchQueue.main.async { completion(text) }
    }
    
    // MARK: – Callback handlers
    private func handlePreprocess(jsonPtr: UnsafeMutablePointer<CChar>?) {
        // Not surfaced in the UI, but it is the phase the transfer sits in
        // while it reads "Preparing transfer…", so log it.
        if let jsonPtr {
            DebugLog.write("preprocess: \(String(cString: jsonPtr).prefix(300))")
        }
    }
    
    private func handleProgress(jsonPtr: UnsafeMutablePointer<CChar>?) {
        guard let jsonPtr else { return }
        guard let kind = transferKind else {
            // Progress arriving with no transfer recorded means the slot was
            // cleared while the backend was still sending bytes, which shows up
            // as a progress bar frozen part way through a copy that is in fact
            // still running.
            DebugLog.write("WARNING: progress dropped, no transfer in flight (op=\(operation))")
            return
        }
        
        let jsonString = String(cString: jsonPtr)
        let (errorString, dataAny) = parseEnvelope(jsonString)
        if let errorString {
            DebugLog.write("progress error under transfer=\(kind): \(errorString)")
            // Cancel errors are handled by the done callback; ignore in progress.
            if ErrorStringLocalizer.isTransferCancelledError(errorString) {
                // Don't clear the transfer slot here. The done callback will
                // handle the full cleanup including reconnection.
                let localizedError = ErrorStringLocalizer.localize(errorString)
                self.errorMessage = localizedError
                return
            }
            DispatchQueue.main.async { [weak self] in
                guard let self else { return }
                if ErrorStringLocalizer.isDeviceDisconnectedError(errorString) {
                    self.handleDeviceDisconnected()
                } else {
                    if kind != .silentDownloading {
                        self.isTransferActive = false
                        self.transferProgress = nil
                        self.transferStats = nil
                        let localizedError = ErrorStringLocalizer.localize(errorString)
                        self.connectionState = .error(localizedError)
                        self.errorMessage = localizedError
                    } else {
                        self.silentTransferStats = nil
                    }
                }
            }
            transferKind = nil
            return
        }
        guard let dataAny else {
            DebugLog.write("progress had no data payload")
            return
        }
        guard let progressData = decodeFullTransferProgress(from: dataAny) else { return }
        let stats = TransferStatistics(progressData: progressData)

        let now = Date()
        if lastProgressLogAt.map({ now.timeIntervalSince($0) >= 2 }) ?? true {
            lastProgressLogAt = now
            let sent = progressData.bulkFileSize?.sent ?? -1
            let total = progressData.bulkFileSize?.total ?? -1
            DebugLog.write("progress phase=\(stats.phase) \(Int(stats.progressPercentage * 100))% \(sent)/\(total) file=\(stats.currentFileName)")
        }
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.lastProgressAt = Date()
            if kind == .silentDownloading {
                self.silentTransferStats = stats
            } else {
                self.transferStats = stats
                self.transferProgress = stats.progressPercentage
            }
        }
    }
    
    /// Independent handler for device list results. Does NOT interact with `operation`,
    /// so it can safely execute while Walk/Upload/Download/etc. are in progress.
    private func handleDeviceListDone(jsonPtr: UnsafeMutablePointer<CChar>?) {
        guard let jsonPtr else { return }
        let jsonString = String(cString: jsonPtr)
        
        if let errorString = parseEnvelopeErrorOnly(jsonString) {
            print("MTPManager: Error fetching available devices: \(errorString)")
            return
        }
        
        guard let dataAny = parseEnvelopeData(jsonString) else { return }
        let d = dataFromAny(dataAny)
        guard let devices = try? JSONDecoder().decode([MTPDeviceInfo].self, from: d) else {
            print("MTPManager: Failed to decode available devices payload")
            DispatchQueue.main.async { [weak self] in
                self?.availableDevices = []
                self?.handleDeviceDisconnected()
            }
            return
        }
        
        print("MTPManager: Found \(devices.count) devices")
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.availableDevices = devices

            // A device that has physically gone away gets a clean slate: the
            // give-up counter must not survive an unplug/replug cycle.
            if devices.isEmpty {
                self.autoConnectFailureDeviceId = ""
                self.autoConnectFailureCount = 0
            }
            
            // Check if the currently connected device disappeared from the list.
            // This handles physical USB disconnection when no MTP operation is in flight.
            let isCurrentlyConnected: Bool = {
                if case .connected = self.connectionState { return true }
                if case .connecting = self.connectionState { return true }
                return false
            }()
            
            if isCurrentlyConnected && !self.deviceId.isEmpty {
                let stillPresent = devices.contains(where: { $0.id == self.deviceId })
                if !stillPresent {
                    print("MTPManager: Active device \(self.deviceId) no longer present — disconnecting")
                    self.handleDeviceDisconnected()
                    self.operation = .none
                    
                    // If there are remaining devices, auto-connect to the first one.
                    if let nextDevice = devices.first, self.mayAutoConnect(to: nextDevice.id) {
                        print("MTPManager: Auto-connecting to remaining device \(nextDevice.id)")
                        self.switchDevice(to: nextDevice.id)
                    }
                    return
                }
            }
            
            // Auto-connect logic: if disconnected and devices available, connect to the first one.
            if case .disconnected = self.connectionState, self.operation != .initializing, let first = devices.first,
               self.mayAutoConnect(to: first.id) {
                print("MTPManager: Auto-connecting to \(first.id)")
                self.switchDevice(to: first.id)
            }
        }
    }
    
    private func handleDone(jsonPtr: UnsafeMutablePointer<CChar>?) {
        guard let jsonPtr else { return }
        
        let jsonString = String(cString: jsonPtr)
        DebugLog.write("done callback under op=\(operation) payload=\(jsonString.prefix(400))")
        
        switch operation {
        case .initializing:
            if let errorString = parseEnvelopeErrorOnly(jsonString) {
                let failedDeviceId = self.deviceId
                DispatchQueue.main.async {
                    self.noteAutoConnectOutcome(deviceId: failedDeviceId, succeeded: false)
                    if ErrorStringLocalizer.isDeviceDisconnectedError(errorString) {
                        self.handleDeviceDisconnected()
                    } else {
                        let localizedError = ErrorStringLocalizer.localize(errorString)
                        self.connectionState = .error(localizedError)
                        self.errorMessage = localizedError
                    }
                }
                operation = .none
                return
            }

            do {
                let connectedDeviceId = self.deviceId
                DispatchQueue.main.async {
                    self.noteAutoConnectOutcome(deviceId: connectedDeviceId, succeeded: true)
                }
            }
            
            // Extract device info from the Initialize response
            if let dataAny = parseEnvelopeData(jsonString) {
                if let deviceInfo = parseDeviceInfo(dataAny) {
                    self.cachedDeviceInfo = deviceInfo
                }
                if let dict = dataAny as? [String: Any] {
                    self.cachedProfile = DeviceProfile(backendValue: dict["deviceProfile"] as? String)
                    self.cachedAdvice = dict["advice"] as? String ?? ""
                }
            }
            
            operation = .fetchingStorages
            let fetchStoragesInput: [String: Any] = ["deviceId": self.deviceId]
            if let fetchJson = self.toJsonString(fetchStoragesInput) {
                fetchJson.withCString { ptr in
                    NxmtpFetchStorages(ptr, CallbackRouter.done)
                }
            }
            
        case .fetchingStorages:
            if let errorString = parseEnvelopeErrorOnly(jsonString) {
                DispatchQueue.main.async {
                    let localizedError = ErrorStringLocalizer.localize(errorString)
                    self.connectionState = .error(localizedError)
                    self.errorMessage = localizedError
                }
                operation = .none
                return
            }
            
            guard let dataAny = parseEnvelopeData(jsonString) else {
                DispatchQueue.main.async {
                    let localizedError = String(localized: "Invalid storages payload")
                    self.connectionState = .error(localizedError)
                }
                operation = .none
                return
            }
            
            let storages = parseStorages(dataAny)
            let first = storages.first(where: { $0.capabilities.browse }) ?? storages.first
            guard let first else {
                DispatchQueue.main.async {
                    let localizedError = String(localized: "No storages found")
                    self.connectionState = .error(localizedError)
                    self.errorMessage = localizedError
                }
                operation = .none
                return
            }
            
            // Use cached device info from initialization, or fallback to defaults
            let deviceName = self.cachedDeviceInfo?.name ?? "MTP Device"
            let deviceManufacturer = self.cachedDeviceInfo?.manufacturer ?? ""
            
            let detectedUSB: USBMonitor.USBDetectionResult = self.usbMonitor?.detectCurrentUSBProtocol(
                productHint: deviceName,
                manufacturerHint: deviceManufacturer
            )
                ?? (protocolName: "Unknown", speedMbps: 0, maxSpeedBytesPerSecond: 42_000_000)
            let usbProtocol = self.mapUSBProtocol(from: detectedUSB.protocolName)
            
            let device = MTPDevice(
                id: "device-1",
                name: deviceName,
                manufacturer: deviceManufacturer,
                storages: storages,
                usbProtocol: usbProtocol,
                usbSpeedMbps: detectedUSB.speedMbps,
                maxSpeedBytesPerSecond: detectedUSB.maxSpeedBytesPerSecond,
                profile: self.cachedProfile,
                advice: self.cachedAdvice
            )
            DispatchQueue.main.async {
                self.connectionState = .connected(device)
                self.selectedStorage = first
                // `operation` must be cleared on the main queue, in the same
                // block that arms the follow-up walk. Clearing it on the
                // callback thread opens a window in which `isDeviceIdle` is
                // true but a walk is about to claim the state machine, and an
                // install-queue drain that slipped into that window would have
                // its `operation` overwritten -- silently dropping every
                // progress callback for the upload it just started.
                //
                // It must also be cleared *before* the walk is armed, never
                // after: `loadFiles(at:)` claims the state machine by setting
                // `operation = .walking`, so clearing afterwards overwrites it
                // and the walk's completion callback is dispatched as `.none`.
                // The listing then never arrives and the browser spins on
                // "Loading files…" forever.
                self.operation = .none
                if let pendingPath = self.pendingNavigationPath {
                    self.pendingNavigationPath = nil
                    self.navigateToPath(pendingPath)
                } else {
                    self.navigationStack = ["/"]
                    self.loadFiles(at: "/")
                }
                // A queue can outlive a disconnect: items stay `.waiting` while
                // the drain loop deliberately stops itself. Re-arm it now that
                // the console is back, or they would wait forever.
                self.scheduleInstallQueueDrain()
            }
            
        case .walking:
            // Check if this walk was superseded by a newer directory request.
            if let dict = try? JSONSerialization.jsonObject(with: Data(jsonString.utf8)) as? [String: Any],
            let errorType = dict["errorType"] as? String,
            errorType == "ErrorWalkCancelled" {
                return
            }
            
            if let errorString = parseEnvelopeErrorOnly(jsonString) {
                DispatchQueue.main.async {
                    if ErrorStringLocalizer.isDeviceDisconnectedError(errorString) {
                        self.handleDeviceDisconnected()
                    } else {
                        self.isLoading = false
                        let localizedError = ErrorStringLocalizer.localize(errorString)
                        self.connectionState = .error(localizedError)
                        self.errorMessage = localizedError
                    }
                }
                operation = .none
                return
            }

            guard let filesAny = parseEnvelopeData(jsonString) else {
                DispatchQueue.main.async { self.isLoading = false }
                operation = .none
                return
            }
            
            let filesData = dataFromAny(filesAny)
            if filesData.isEmpty {
                // e.g. empty directory listing: gomtp typically returns `data: []`.
                DispatchQueue.main.async {
                    self.files = []
                    self.isLoading = false
                    self.showingCachedListing = false
                }
                if let walked = walkingPath, let storage = selectedStorage {
                    listingCache[listingCacheKey(storage: storage, path: walked)] = []
                }
                walkingPath = nil
                operation = .none
                return
            }
            guard let decoded = try? JSONDecoder().decode([NxmtpWalkFileInfo].self, from: filesData) else {
                DispatchQueue.main.async { self.isLoading = false }
                operation = .none
                return
            }
            
            let mappedFiles: [MTPFile] = decoded.map { fi in
                MTPFile(
                    id: String(fi.objectId),
                    name: fi.name,
                    size: fi.size,
                    dateModified: self.parseNxmtpDate(fi.dateAdded),
                    isDirectory: fi.isFolder,
                    path: fi.path,
                    extension_: fi.extension_,
                    sizeUnknown: fi.sizeUnknown ?? false
                )
            }
            
            DispatchQueue.main.async {
                self.files = mappedFiles
                self.isLoading = false
                self.showingCachedListing = false
            }
            if let walked = walkingPath, let storage = selectedStorage {
                listingCache[listingCacheKey(storage: storage, path: walked)] = mappedFiles
            }
            walkingPath = nil
            operation = .none
            
        case .deleting, .makingDirectory, .renaming:
            if let errorString = parseEnvelopeErrorOnly(jsonString) {
                DispatchQueue.main.async {
                    if ErrorStringLocalizer.isDeviceDisconnectedError(errorString) {
                        self.handleDeviceDisconnected()
                    } else {
                        let localizedError = ErrorStringLocalizer.localize(errorString)
                        self.connectionState = .error(localizedError)
                        self.errorMessage = localizedError
                    }
                }
                operation = .none
                return
            }
            
            DispatchQueue.main.async { [weak self] in
                guard let self else { return }
                // Cleared here, not on the callback thread: see the note in
                // `.fetchingStorages`. Between clearing `operation` and
                // `loadFiles` claiming it, `isDeviceIdle` reports true, and an
                // install-queue drain landing in that window would lose every
                // progress callback for the upload it started. Clearing must
                // also come *before* the walk, or it overwrites the `.walking`
                // state the walk just claimed and the listing never arrives.
                self.operation = .none
                self.loadFiles(at: self.currentPath)
            }
            
        case .disposing:
            DispatchQueue.main.async {
                self.connectionState = .disconnected
                self.files = []
                self.navigationStack = []
                self.selectedStorage = nil
                self.isTransferActive = false
                self.transferProgress = nil
                self.transferStats = nil
                self.errorMessage = nil
                self.isLoading = false
                self.cachedDeviceInfo = nil
            }
            // The session is gone, so any transfer it was running is gone with
            // it. Clearing the slot keeps `isDeviceIdle` honest for the next
            // connection.
            transferKind = nil
            operation = .none
            if let nextDeviceId = pendingDeviceId {
                pendingDeviceId = nil
                self.deviceId = nextDeviceId
                self.connectDevice()
            }
            
        case .none:
            // A callback with no operation claimed means whatever armed it
            // cleared the state machine too early, so the result is being
            // thrown away. This has been a real bug twice; make it visible.
            DebugLog.write("WARNING: done callback swallowed under .none")
            break
        }
    }

    /// Completion for uploads and downloads, routed away from `operation` so a
    /// walk armed mid-transfer cannot consume it. See `TransferKind`.
    private func handleTransferDone(jsonPtr: UnsafeMutablePointer<CChar>?) {
        guard let jsonPtr else { return }
        let jsonString = String(cString: jsonPtr)

        guard let kind = transferKind else {
            DebugLog.write("WARNING: transfer done with no transfer in flight")
            return
        }
        transferKind = nil
        if kind == .uploading, let key = uploadDestinationCacheKey {
            listingCache[key] = nil
            uploadDestinationCacheKey = nil
        }

        if kind == .silentDownloading {
            if let errorString = parseEnvelopeErrorOnly(jsonString) {
                self.finishTransferCompletion(errorString: errorString)
                DispatchQueue.main.async { [weak self] in
                    self?.silentTransferStats = nil
                    if ErrorStringLocalizer.isDeviceDisconnectedError(errorString) {
                        self?.handleDeviceDisconnected()
                    }
                }
                return
            }
            self.finishTransferCompletion(errorString: nil)
            DispatchQueue.main.async { [weak self] in
                self?.silentTransferStats = nil
            }
            return
        }

        DebugLog.write("transfer done: \(jsonString)")
        if let errorString = parseEnvelopeErrorOnly(jsonString) {
            self.finishTransferCompletion(errorString: errorString)
            DispatchQueue.main.async {
                if ErrorStringLocalizer.isDeviceDisconnectedError(errorString) {
                    self.finishActiveInstall(errorString: errorString)
                    self.handleDeviceDisconnected()
                } else if ErrorStringLocalizer.isTransferCancelledError(errorString) {
                    // User-initiated cancel: the MTP session is corrupt after
                    // cancellation (broken transaction IDs, stale USB data).
                    // Dispose and reconnect to reset the session.
                    self.isTransferActive = false
                    self.transferProgress = nil
                    self.transferStats = nil
                    self.lastProgressAt = nil
                    let localizedError = ErrorStringLocalizer.localize(errorString)
                    self.errorMessage = localizedError
                    self.finishActiveInstall(errorString: errorString)
                    self.pendingNavigationPath = nil
                    self.reconnectAndRestore(to: self.currentPath)
                } else {
                    self.isTransferActive = false
                    self.transferProgress = nil
                    self.transferStats = nil
                    self.lastProgressAt = nil
                    let localizedError = ErrorStringLocalizer.localize(errorString)
                    self.connectionState = .error(localizedError)
                    self.errorMessage = localizedError
                    self.finishActiveInstall(errorString: errorString)
                    self.runPendingNavigation()
                }
            }
            return
        }

        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.isTransferActive = false
            self.transferProgress = nil
            self.transferStats = nil
            self.lastProgressAt = nil
            self.finishActiveInstall(errorString: nil)
            // A folder the user asked for mid-transfer takes precedence over
            // re-listing the one they were on, which they have already left.
            if !self.runPendingNavigation() {
                self.loadFiles(at: self.currentPath)
            }

            if !NSApplication.shared.isActive {
                let content = UNMutableNotificationContent()
                content.title = String(localized: "Transfer Complete")
                content.body = kind == .uploading ? String(localized: "Import completed successfully.") : String(localized: "Export completed successfully.")
                let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
                UNUserNotificationCenter.current().requestAuthorization(options: [.alert]) { granted, _ in
                    if granted {
                        UNUserNotificationCenter.current().add(request)
                    }
                }
            }
        }
    }
    
    // MARK: – Device Connection
    func connectDevice() {
        // Reachable only after a device scan, which is itself gated -- but the
        // menu bar stays live behind the first-run disclosure, so the guard is
        // repeated here rather than relying on that chain holding.
        guard isStarted else { return }

        // Connect stays enabled with nothing attached, because retrying after
        // waking the console or starting the responder is the normal thing to
        // do. Without this the empty id reached ParseDeviceID, which failed and
        // put its own diagnostics on screen -- "malformed device id" is not an
        // answer to "why can't it see my Switch", and it is the first thing
        // anyone who opens the app before launching DBI would have seen.
        var target = deviceId
        if target.isEmpty {
            guard let candidate = availableDevices.first else {
                DispatchQueue.main.async {
                    self.connectionState = .disconnected
                    self.errorMessage = String(
                        localized: "No console found. Check the cable, then launch DBI and choose \"Run MTP responder\".",
                        comment: "Shown when Connect is pressed with no device attached"
                    )
                }
                return
            }
            target = candidate.id
            deviceId = target
        }

        DispatchQueue.main.async {
            // An explicit connect is always honoured: clear any auto-connect
            // give-up state so the user is never locked out by the guard.
            self.autoConnectFailureDeviceId = ""
            self.autoConnectFailureCount = 0
            self.connectionState = .connecting
            self.errorMessage = nil
        }
        
        operation = .initializing
        let input: [String: Any] = ["deviceId": target]
        if let jsonString = toJsonString(input) {
            DispatchQueue.global(qos: .userInitiated).async {
                jsonString.withCString { ptr in
                    NxmtpInitialize(ptr, CallbackRouter.done)
                }
            }
        }
    }
    
    func disconnectDevice() {
        operation = .disposing
        let input: [String: Any] = ["deviceId": self.deviceId]
        if let jsonString = toJsonString(input) {
            DispatchQueue.global(qos: .userInitiated).async {
                jsonString.withCString { ptr in
                    NxmtpDispose(ptr, CallbackRouter.done)
                }
            }
        }
    }
    
    // MARK: – USB Hotplug & Retry Logic
    private let maxRetryCount: Int = 1
    private let retryDelay: TimeInterval = 2.0
    private let initialCheckDelay: TimeInterval = 1.5  // Initial delay before checking connection status
    
    /// Handles USB device attachment/detachment events
    private func handleUSBEvent(attached: Bool) {
        DispatchQueue.main.async { [weak self] in
            self?.errorMessage = nil
            if case .error = self?.connectionState {
                self?.connectionState = .disconnected
            }
        }
        
        // Debounce USB events - many events fire rapidly during plug/unplug.
        // This prevents rapid-fire scans that could overwhelm the USB bus.
        deviceScanWorkItem?.cancel()
        let workItem = DispatchWorkItem { [weak self] in
            self?.fetchAvailableDevices()
        }
        deviceScanWorkItem = workItem
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.8, execute: workItem)
    }
    
    /// Attempts to connect with retry logic
    private func attemptConnectWithRetry() {
        // Check if already connected
        if case .connected = connectionState {
            print("MTPManager: Already connected, skipping retry")
            hotplugRetryCount = 0
            return
        }
        
        // Attempt connection
        connectDevice()
        
        // Register to monitor for success/failure
        // We'll check the connection state after a delay to allow device initialization
        DispatchQueue.main.asyncAfter(deadline: .now() + initialCheckDelay) { [weak self] in
            self?.checkConnectionAndRetry()
        }
    }
    
    /// Checks if connection succeeded, retries if needed
    private func checkConnectionAndRetry() {
        // If connected, reset retry count and return
        if case .connected = connectionState {
            print("MTPManager: Connection successful, retry count reset")
            hotplugRetryCount = 0
            return
        }
        
        // If failed, attempt retry
        hotplugRetryCount += 1
        print("MTPManager: Connection failed, retry attempt \(hotplugRetryCount)/\(maxRetryCount)")
        
        if hotplugRetryCount >= maxRetryCount {
            print("MTPManager: Max retries reached, giving up")
            hotplugRetryCount = 0
            return
        }
        
        // Schedule next retry
        retryTimer?.invalidate()
        retryTimer = Timer.scheduledTimer(withTimeInterval: retryDelay, repeats: false) { [weak self] _ in
            print("MTPManager: Attempting retry \(self?.hotplugRetryCount ?? 0 + 1)...")
            self?.attemptConnectWithRetry()
        }
    }
    
    // MARK: – Navigation

    private func listingCacheKey(storage: MTPStorage, path: String) -> String {
        "\(storage.id)|\(path)"
    }

    /// Replays a folder the user asked for while the device was busy.
    /// Returns true when a navigation was pending, so callers can skip their
    /// own reload rather than listing a folder the user has already left.
    @discardableResult
    func runPendingNavigation() -> Bool {
        guard let path = pendingNavigationPath else { return false }
        pendingNavigationPath = nil
        loadFiles(at: path)
        return true
    }

    func navigate(to directory: MTPFile) {
        guard directory.isDirectory else { return }
        let newPath = currentPath == "/" ? "/\(directory.name)" : "\(currentPath)/\(directory.name)"
        navigationStack.append(newPath)
        loadFiles(at: newPath)
    }
    
    func navigateBack() {
        guard canGoBack else { return }
        navigationStack.removeLast()
        loadFiles(at: currentPath)
    }
    
    func navigate(toIndex index: Int) {
        guard index < navigationStack.count else { return }
        navigationStack = Array(navigationStack.prefix(index + 1))
        loadFiles(at: currentPath)
    }

    /// Navigate directly to an absolute path (e.g. "/Pictures/Screenshots").
    /// Builds the full breadcrumb navigation stack from "/" to the target.
    func navigateToPath(_ path: String) {
        guard selectedStorage != nil else { return }
        var stack: [String] = ["/"]
        let components = path.split(separator: "/").map(String.init)
        var current = ""
        for component in components {
            current += "/\(component)"
            stack.append(current)
        }
        navigationStack = stack
        loadFiles(at: path)
    }
    
    // MARK: – File Listing
    func loadFiles(at path: String) {
        guard let storage = selectedStorage else { return }
        guard storage.id != "" else { return }

        // MTP is a single session: `nxmtp.Client` serialises every operation on
        // one mutex that an upload or download holds for its entire duration.
        // Issuing the walk now would block a background thread until the copy
        // finished and leave the user staring at a spinner, so remember where
        // they wanted to go and take them there once the device is free. A
        // folder already visited is shown from cache meanwhile -- an install
        // queue can run for hours, and browsing should not be dead for all of
        // it just because the console cannot answer right now.
        if transferKind != nil {
            let key = listingCacheKey(storage: storage, path: path)
            let cached = listingCache[key]
            DebugLog.write("navigation deferred until transfer completes: \(path)\(cached == nil ? "" : " (showing cached listing)")")
            pendingNavigationPath = path
            DispatchQueue.main.async { [weak self] in
                guard let self else { return }
                self.isLoading = false
                self.files = cached ?? []
                self.showingCachedListing = cached != nil
            }
            return
        }

        walkingPath = path
        DispatchQueue.main.async { [weak self] in
            self?.isLoading = true
        }
        
        let storageId = self.uint32FromStorageId(storage.id)
        let skipHiddenFiles = false
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "fullPath": path,
            "recursive": false,
            "skipDisallowedFiles": false,
            "skipHiddenFiles": skipHiddenFiles
        ]
        
        guard let jsonString = self.toJsonString(input) else {
            DispatchQueue.main.async { [weak self] in
                self?.isLoading = false
            }
            return
        }
        
        operation = .walking
        DispatchQueue.global(qos: .userInitiated).async {
            jsonString.withCString { ptr in
                NxmtpWalk(ptr, CallbackRouter.done)
            }
        }
    }
    
    // MARK: – Transfer
    func download(files: [MTPFile], destinationURL: URL) {
        download(paths: files.map(\.path), destinationURL: destinationURL, from: nil)
    }

    /// Downloads by path from an explicit storage.
    ///
    /// `storage` defaults to the browsed storage. The Switch workflows need to
    /// read from Saves/Album/Gamecard without navigating there first, and
    /// silently retargeting `selectedStorage` would move the user's view out
    /// from under them.
    func download(paths: [String], destinationURL: URL, from storage: MTPStorage?) {
        guard !paths.isEmpty else {
            DebugLog.write("download aborted: no paths")
            return
        }
        guard let storage = storage ?? selectedStorage else {
            DebugLog.write("download aborted: no storage")
            return
        }
        guard !isTransferInFlight else {
            DebugLog.write("download refused: a transfer is already running")
            DispatchQueue.main.async { [weak self] in
                self?.errorMessage = String(localized: "The console is already busy with a transfer. Wait for it to finish, then try again.")
            }
            return
        }
        DebugLog.write("download storage=\(storage.id) paths=\(paths) dest=\(destinationURL.path) op=\(operation)")
        
        let storageId = self.uint32FromStorageId(storage.id)
        let sources = paths
        let destination = destinationURL.path
        
        DispatchQueue.main.async { [weak self] in
            self?.isTransferActive = true
            self?.transferProgress = 0
        }
        beginSecurityScopedAccess(destinationURL: destinationURL)
        
        let preprocessFiles = true
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "sources": sources,
            "destination": destination,
            "preprocessFiles": preprocessFiles
        ]
        
        guard let jsonString = toJsonString(input) else {
            DispatchQueue.main.async { [weak self] in
                self?.isTransferActive = false
                self?.transferProgress = nil
            }
            endSecurityScopedAccess()
            return
        }
        transferKind = .downloading
        DispatchQueue.global(qos: .userInitiated).async {
            DebugLog.write("download -> NxmtpDownloadFiles \(jsonString)")
            jsonString.withCString { ptr in
                NxmtpDownloadFiles(ptr, CallbackRouter.preprocess, CallbackRouter.progress, CallbackRouter.transferDone)
            }
            DebugLog.write("download <- NxmtpDownloadFiles returned")
        }
    }
    
    func upload(sourceURLs: [URL]) {
        upload(sourceURLs: sourceURLs, to: nil, destination: nil)
    }

    /// Uploads to an explicit storage and destination directory.
    ///
    /// Both default to what the user is browsing. The install drop targets pass
    /// the install storage and "/" explicitly: DBI begins installing as soon as
    /// the object lands in the storage root, and those storages cannot be
    /// browsed, so there is no current path to fall back on.
    func upload(sourceURLs: [URL], to storage: MTPStorage?, destination: String?) {
        guard !sourceURLs.isEmpty else {
            DebugLog.write("upload aborted: no sources")
            return
        }
        guard let storage = storage ?? selectedStorage else {
            DebugLog.write("upload aborted: no storage")
            return
        }
        // The toolbar's Import button disables itself during a transfer, but
        // drag-and-drop reaches here directly and used to start a second upload
        // on top of the first. MTP is a single session, so the two serialised on
        // the Go mutex rather than corrupting anything on the card -- but the
        // second claim overwrote the first, and whichever finished last found an
        // empty slot and was discarded. `isTransferActive` then stayed true for
        // the life of the process, so the install queue never drained again.
        guard transferKind == nil, !isTransferActive else {
            DebugLog.write("upload refused: a transfer is already running")
            DispatchQueue.main.async { [weak self] in
                self?.errorMessage = String(localized: "The console is already busy with a transfer. Wait for it to finish, then copy these files again.")
            }
            return
        }
        DispatchQueue.main.async { [weak self] in
            self?.isTransferActive = true
            self?.transferProgress = 0
        }
        beginSecurityScopedAccess(sourceURLs: sourceURLs)
        let storageId = uint32FromStorageId(storage.id)
        let sources = sourceURLs.map(\.path)
        let destination = destination ?? currentPath
        
        let preprocessFiles = true
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "sources": sources,
            "destination": destination,
            "preprocessFiles": preprocessFiles
        ]
        
        guard let jsonString = toJsonString(input) else {
            DebugLog.write("upload aborted: could not encode request")
            DispatchQueue.main.async { [weak self] in
                self?.isTransferActive = false
                self?.transferProgress = nil
            }
            endSecurityScopedAccess()
            return
        }
        let sizes = sourceURLs.map { url -> String in
            let bytes = (try? FileManager.default.attributesOfItem(atPath: url.path)[.size] as? Int64) ?? nil
            return "\(url.lastPathComponent)=\(bytes.map(String.init) ?? "?")"
        }
        DebugLog.write("upload storage=\(storage.id) dest=\(destination) op=\(operation) files=[\(sizes.joined(separator: ", "))]")
        transferKind = .uploading
        uploadDestinationCacheKey = listingCacheKey(storage: storage, path: destination)
        DispatchQueue.global(qos: .userInitiated).async {
            DebugLog.write("upload -> NxmtpUploadFiles \(jsonString)")
            jsonString.withCString { ptr in
                NxmtpUploadFiles(ptr, CallbackRouter.preprocess, CallbackRouter.progress, CallbackRouter.transferDone)
            }
            DebugLog.write("upload <- NxmtpUploadFiles returned")
        }
    }
    
    func downloadPromise(file: MTPFile, to destinationFolderURL: URL, completion: @escaping (Error?) -> Void) {
        if isTransferInFlight {
            completion(NSError(domain: "MTPManager.Transfer", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "Another transfer is already running."
            ]))
            return
        }
        guard let storage = selectedStorage else {
            completion(NSError(domain: "MTPManager.Transfer", code: 2, userInfo: [
                NSLocalizedDescriptionKey: "No storage selected."
            ]))
            return
        }
        
        transferCompletion = completion
        DispatchQueue.main.async { [weak self] in
            self?.isTransferActive = true
            self?.transferProgress = 0
        }
        
        let storageId = self.uint32FromStorageId(storage.id)
        let sources = [file.path]
        let destination = destinationFolderURL.path
        let preprocessFiles = true
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "sources": sources,
            "destination": destination,
            "preprocessFiles": preprocessFiles
        ]
        
        guard let jsonString = toJsonString(input) else {
            DispatchQueue.main.async { [weak self] in
                self?.isTransferActive = false
                self?.transferProgress = nil
            }
            finishTransferCompletion(errorString: "Failed to encode download payload.")
            return
        }
        
        transferKind = .downloading
        DispatchQueue.global(qos: .userInitiated).async {
            jsonString.withCString { ptr in
                NxmtpDownloadFiles(ptr, CallbackRouter.preprocess, CallbackRouter.progress, CallbackRouter.transferDone)
            }
        }
    }
    
    /// Download multiple files from device as a batch promise (for drag-to-Finder).
    /// Sends all file paths in a single NxmtpDownloadFiles call for proper "n of n" progress.
    func downloadPromiseBatch(files: [MTPFile], to destinationFolderURL: URL, completion: @escaping (Error?) -> Void) {
        if isTransferInFlight {
            completion(NSError(domain: "MTPManager.Transfer", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "Another transfer is already running."
            ]))
            return
        }
        guard let storage = selectedStorage else {
            completion(NSError(domain: "MTPManager.Transfer", code: 2, userInfo: [
                NSLocalizedDescriptionKey: "No storage selected."
            ]))
            return
        }
        
        transferCompletion = completion
        DispatchQueue.main.async { [weak self] in
            self?.isTransferActive = true
            self?.transferProgress = 0
        }
        
        let storageId = self.uint32FromStorageId(storage.id)
        let sources = files.map(\.path)
        let destination = destinationFolderURL.path
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "sources": sources,
            "destination": destination,
            "preprocessFiles": true
        ]
        
        guard let jsonString = toJsonString(input) else {
            DispatchQueue.main.async { [weak self] in
                self?.isTransferActive = false
                self?.transferProgress = nil
            }
            finishTransferCompletion(errorString: "Failed to encode download payload.")
            return
        }
        
        transferKind = .downloading
        DispatchQueue.global(qos: .userInitiated).async {
            jsonString.withCString { ptr in
                NxmtpDownloadFiles(ptr, CallbackRouter.preprocess, CallbackRouter.progress, CallbackRouter.transferDone)
            }
        }
    }
    
    func downloadAndPreview(file: MTPFile, to destinationFolderURL: URL, completion: @escaping (Error?) -> Void) {
        // `operation` alone is not enough: transfers deliberately live in
        // `transferKind`, so during an upload `operation` reads `.none` and a
        // double-click here would start a preview download on top of it.
        if isTransferInFlight || (operation != .none && operation != .walking) {
            completion(NSError(domain: "MTPManager.Transfer", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "Another operation is running."
            ]))
            return
        }
        guard let storage = selectedStorage else {
            completion(NSError(domain: "MTPManager.Transfer", code: 2, userInfo: [
                NSLocalizedDescriptionKey: "No storage selected."
            ]))
            return
        }
        
        transferCompletion = completion
        
        let storageId = self.uint32FromStorageId(storage.id)
        let sources = [file.path]
        let destination = destinationFolderURL.path
        let preprocessFiles = true
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "sources": sources,
            "destination": destination,
            "preprocessFiles": preprocessFiles
        ]
        
        guard let jsonString = toJsonString(input) else {
            finishTransferCompletion(errorString: "Failed to encode download payload.")
            return
        }
        
        transferKind = .silentDownloading
        DispatchQueue.global(qos: .userInitiated).async {
            jsonString.withCString { ptr in
                NxmtpDownloadFiles(ptr, CallbackRouter.preprocess, CallbackRouter.progress, CallbackRouter.transferDone)
            }
        }
    }
    
    func deleteFiles(_ filesToDelete: [MTPFile]) {
        guard !filesToDelete.isEmpty else { return }
        guard let storage = selectedStorage else { return }
        
        let storageId = uint32FromStorageId(storage.id)
        let filePaths = filesToDelete.map(\.path)
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "files": filePaths
        ]
        
        guard let jsonString = toJsonString(input) else { return }
        operation = .deleting
        DispatchQueue.global(qos: .userInitiated).async {
            jsonString.withCString { ptr in
                NxmtpDeleteFile(ptr, CallbackRouter.done)
            }
        }
    }
    
    func createFolder(named name: String) {
        guard let storage = selectedStorage else { return }
        
        if files.contains(where: { $0.name == name }) {
            DispatchQueue.main.async { [weak self] in
                self?.isShowingNameConflictAlert = true
            }
            return
        }
        
        let storageId = uint32FromStorageId(storage.id)
        let fullPath = currentPath == "/" ? "/\(name)" : "\(currentPath)/\(name)"
        
        DispatchQueue.main.async { [weak self] in
            self?.isLoading = true
        }
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "fullPath": fullPath
        ]
        guard let jsonString = toJsonString(input) else { return }
        operation = .makingDirectory
        DispatchQueue.global(qos: .userInitiated).async {
            jsonString.withCString { ptr in
                NxmtpMakeDirectory(ptr, CallbackRouter.done)
            }
        }
    }
    
    func renameFile(_ file: MTPFile, to newName: String) {
        guard let storage = selectedStorage else { return }
        
        if file.name != newName && files.contains(where: { $0.name == newName }) {
            DispatchQueue.main.async { [weak self] in
                self?.isShowingNameConflictAlert = true
            }
            return
        }
        
        let storageId = uint32FromStorageId(storage.id)
        
        DispatchQueue.main.async { [weak self] in
            self?.isLoading = true
        }
        
        let input: [String: Any] = [
            "deviceId": self.deviceId,
            "storageId": Int(storageId),
            "fullPath": file.path,
            "newFileName": newName
        ]
        
        guard let jsonString = toJsonString(input) else { return }
        operation = .renaming
        DispatchQueue.global(qos: .userInitiated).async {
            jsonString.withCString { ptr in
                NxmtpRenameFile(ptr, CallbackRouter.done)
            }
        }
    }
    
    // MARK: – Cancel Transfer
    func cancelTransfer() {
        guard isTransferActive else { return }
        
        let input: [String: Any] = ["deviceId": self.deviceId]
        guard let jsonString = toJsonString(input) else { return }
        
        // Fire-and-forget: CancelTransfer uses its own callback that does NOT
        // go through the operation state machine (similar to FetchAvailableDevices).
        DispatchQueue.global(qos: .userInitiated).async {
            jsonString.withCString { ptr in
                NxmtpCancelTransfer(ptr, CallbackRouter.cancelDone)
            }
        }
    }
    
    private func handleCancelDone(jsonPtr: UnsafeMutablePointer<CChar>?) {
        guard let jsonPtr else { return }
        let jsonString = String(cString: jsonPtr)
        
        if let errorString = parseEnvelopeErrorOnly(jsonString) {
            print("MTPManager: CancelTransfer error: \(errorString)")
        } else {
            print("MTPManager: Transfer cancelled successfully")
        }
        // The actual transfer operation's done callback will fire separately
        // with a cancellation error, which handleDone will process normally.
        // We don't reset UI state here — let the transfer's own error handling do it.
    }
    
    /// After a user-initiated cancel, the MTP session is left in a corrupt state
    /// (broken transaction IDs, pending USB data, or invalid directory access). We must 
    /// dispose and reconnect to reset the protocol state, then navigate to the target path.
    func reconnectAndRestore(to targetPath: String) {
        let savedPath = targetPath
        let savedDeviceId = self.deviceId
        
        // Save the path to restore after reconnection
        pendingNavigationPath = savedPath
        
        // Queue reconnection to the same device after dispose completes
        pendingDeviceId = savedDeviceId
        
        // Show connecting state while we reset
        connectionState = .connecting
        
        // Dispose the corrupt MTP session.
        // handleDone(.disposing) will see pendingDeviceId and call connectDevice().
        // handleDone(.fetchingStorages) will see pendingNavigationPath and navigate there.
        operation = .disposing
        let input: [String: Any] = ["deviceId": savedDeviceId]
        if let jsonString = toJsonString(input) {
            DispatchQueue.global(qos: .userInitiated).async {
                jsonString.withCString { ptr in
                    NxmtpDispose(ptr, CallbackRouter.done)
                }
            }
        }
    }
    
    // MARK: – Helpers
    private func isDeviceDisconnectedError(_ error: String) -> Bool {
        // Electron uses `errorType` to detect device changed / lost device.
        // Our `parseEnvelopeErrorOnly` may format it as: `ErrorDeviceChanged: ...`
        return error.contains("ErrorDeviceChanged") || error.contains("LIBUSB_ERROR_NO_DEVICE")
    }
    
    private func handleDeviceDisconnected() {
        connectionState = .disconnected
        files = []
        navigationStack = []
        selectedStorage = nil
        isLoading = false
        isTransferActive = false
        transferProgress = nil
        transferStats = nil
        // A disconnect can pre-empt the transfer's own done callback -- the very
        // case the note below describes for `operation`. Leaving the slot set
        // would keep `isDeviceIdle` false for the life of the process, so the
        // install queue would never drain again and every navigation would be
        // deferred forever waiting on a transfer that has already died.
        transferKind = nil
        pendingNavigationPath = nil
        // A different card could be behind the next connection, so nothing kept
        // from this one can be trusted.
        listingCache.removeAll()
        walkingPath = nil
        showingCachedListing = false
        uploadDestinationCacheKey = nil
        finishTransferCompletion(errorString: "Device disconnected.")
        // The queue's only other exit is the upload's own done callback, and a
        // disconnect detected by the USB scan resets `operation` before that
        // callback arrives -- so it lands in `case .none` and is swallowed.
        // Reconciling here makes every disconnect route release the queue,
        // rather than leaving the running item `.active` for the lifetime of
        // the process with no UI affordance to clear it.
        finishActiveInstall(errorString: "Device disconnected.")
        errorMessage = nil
    }
    
    private func beginSecurityScopedAccess(sourceURLs: [URL]) {
        scopedSourceURLs = sourceURLs.filter { $0.startAccessingSecurityScopedResource() }
    }
    
    private func beginSecurityScopedAccess(destinationURL: URL) {
        if destinationURL.startAccessingSecurityScopedResource() {
            scopedDestinationURL = destinationURL
        }
    }
    
    private func endSecurityScopedAccess() {
        scopedSourceURLs.forEach { $0.stopAccessingSecurityScopedResource() }
        scopedSourceURLs = []
        scopedDestinationURL?.stopAccessingSecurityScopedResource()
        scopedDestinationURL = nil
    }
    
    private func finishTransferCompletion(errorString: String?) {
        endSecurityScopedAccess()
        guard let completion = transferCompletion else { return }
        transferCompletion = nil
        if let errorString {
            completion(NSError(domain: "MTPManager.Transfer", code: 3, userInfo: [
                NSLocalizedDescriptionKey: errorString
            ]))
        } else {
            completion(nil)
        }
    }
    
    private func fetchStorages() {
        operation = .fetchingStorages
        let input: [String: Any] = ["deviceId": self.deviceId]
        if let jsonString = toJsonString(input) {
            DispatchQueue.global(qos: .userInitiated).async {
                jsonString.withCString { ptr in
                    NxmtpFetchStorages(ptr, CallbackRouter.done)
                }
            }
        }
    }
    
    func fetchAvailableDevices() {
        // Nothing may touch the USB bus before the user has acknowledged the
        // disclosure explaining what touching it does.
        guard isStarted else { return }
        // NOTE: Does NOT set `operation` - device scanning is completely independent
        // of the MTP operation state machine. This prevents clobbering in-flight
        // operations (Walk, Upload, etc.) whose callbacks would be misrouted.
        DispatchQueue.global(qos: .userInitiated).async {
            NxmtpFetchAvailableDevices(CallbackRouter.deviceListDone)
        }
    }
    
    func switchDevice(to newDeviceId: String) {
        guard !newDeviceId.isEmpty else { return }
        
        // If already connected to this device, do nothing.
        if case .connected = connectionState, self.deviceId == newDeviceId {
            return
        }
        if case .connecting = connectionState, self.deviceId == newDeviceId {
            return
        }
        
        // If we're already disposing, queue the next device and wait.
        if case .disposing = operation {
            pendingDeviceId = newDeviceId
            return
        }
        
        // Disconnect current device if needed, then connect after dispose completes.
        switch connectionState {
        case .connected, .connecting, .error:
            pendingDeviceId = newDeviceId
            disconnectDevice()
            return
        case .disconnected:
            break
        }
        
        self.deviceId = newDeviceId
        self.connectDevice()
    }
    
    private func uint32FromStorageId(_ storageId: String) -> UInt32 {
        if let v = UInt32(storageId) { return v }
        // Some storages may have non-numeric IDs; best-effort fallback.
        return 0
    }
    
    private func toJsonString(_ object: Any) -> String? {
        guard JSONSerialization.isValidJSONObject(object) else { return nil }
        guard let data = try? JSONSerialization.data(withJSONObject: object, options: []) else { return nil }
        return String(data: data, encoding: .utf8)
    }
    
    private func dataFromAny(_ any: Any) -> Data {
        // In Foundation, `NSJSONSerialization` sometimes directly throws/triggers exceptions
        // for non-JSON container types, try?/Catch may not be able to catch.
        // Use isValidJSONObject for strong verification to avoid the collapse of empty dir or other cases.
        guard JSONSerialization.isValidJSONObject(any) else { return Data() }
        return (try? JSONSerialization.data(withJSONObject: any, options: [])) ?? Data()
    }
    
    private func parseEnvelope(_ jsonString: String) -> (error: String?, data: Any?) {
        guard let data = jsonString.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data, options: []),
              let dict = obj as? [String: Any]
        else {
            return ("Invalid gomtp JSON", nil)
        }
        
        let errorString = (dict["error"] as? String) ?? ""
        // Electron treats `errorType` as `stderr`.
        let errorTypeString = (dict["errorType"] as? String) ?? ""
        let dataAny = dict["data"]
        
        if !errorString.isEmpty && !errorTypeString.isEmpty {
            return ("\(errorTypeString): \(errorString)", dataAny)
        }
        if !errorString.isEmpty {
            return (errorString, dataAny)
        }
        if !errorTypeString.isEmpty {
            return (errorTypeString, dataAny)
        }
        return (nil, dataAny)
    }
    
    private func parseEnvelopeErrorOnly(_ jsonString: String) -> String? {
        let (error, _) = parseEnvelope(jsonString)
        return error
    }
    
    private func parseEnvelopeData(_ jsonString: String) -> Any? {
        let (_, data) = parseEnvelope(jsonString)
        return data
    }
    
    private func parseDeviceInfo(_ any: Any) -> (name: String, manufacturer: String)? {
        // Data structure from Go's send_to_js: { mtpDeviceInfo: {...}, usbDeviceInfo: {...} }
        guard let dict = any as? [String: Any] else { return nil }
        
        // Try to get from mtpDeviceInfo first (MTP protocol info)
        if let mtpInfo = dict["mtpDeviceInfo"] as? [String: Any] {
            let model = mtpInfo["Model"] as? String ?? ""
            let manufacturer = mtpInfo["Manufacturer"] as? String ?? ""
            
            // If we have a model, use it along with manufacturer
            if !model.isEmpty || !manufacturer.isEmpty {
                return (name: model.isEmpty ? "MTP Device" : model, manufacturer: manufacturer)
            }
        }
        
        // Fallback to usbDeviceInfo
        if let usbInfo = dict["usbDeviceInfo"] as? [String: Any] {
            let deviceName = usbInfo["DeviceName"] as? String ?? ""
            let manufacturer = usbInfo["Manufacturer"] as? String ?? ""
            
            if !deviceName.isEmpty || !manufacturer.isEmpty {
                return (name: deviceName.isEmpty ? "Android Device" : deviceName, manufacturer: manufacturer)
            }
        }
        
        return nil
    }
    
    private func parseStorages(_ any: Any) -> [MTPStorage] {
        guard let list = any as? [[String: Any]] else { return [] }
        return list.compactMap { storage in
            let sidInt = int64FromAny(storage["Sid"]) ?? int64FromAny(storage["sid"]) ?? 0
            let id = String(UInt32(max(sidInt, 0)))
            
            let infoAny = storage["Info"] as? [String: Any] ?? storage["info"] as? [String: Any]
            let name = infoAny?["StorageDescription"] as? String ?? "Storage"
            
            let freeSpace = int64FromAny(infoAny?["FreeSpaceInBytes"]) ?? 0
            let totalSpace = int64FromAny(infoAny?["MaxCapability"]) ?? 0
            
            let displayName = storage["displayName"] as? String
            let kind = StorageKind(backendValue: storage["kind"] as? String)
            let caps = StorageCapabilities(json: storage["capabilities"] as? [String: Any])

            return MTPStorage(
                id: id,
                name: (displayName?.isEmpty == false ? displayName! : name),
                freeSpace: freeSpace,
                totalSpace: totalSpace,
                kind: kind,
                capabilities: caps,
                virtual: storage["virtual"] as? Bool ?? false,
                sizeReliable: storage["sizeReliable"] as? Bool ?? true,
                order: (int64FromAny(storage["order"]).map { Int($0) }) ?? 0
            )
        }
        // The backend assigns a stable presentation order (SD card first, system
        // volumes last); preserve it rather than MTP's arbitrary storage-id order.
        .sorted { ($0.order, $0.name) < ($1.order, $1.name) }
    }
    
    private func int64FromAny(_ any: Any?) -> Int64? {
        switch any {
        case let v as Int64: return v
        case let v as UInt64: return Int64(v)
        case let v as Int: return Int64(v)
        case let v as Double: return Int64(v)
        case let v as Float: return Int64(v)
        case let v as NSNumber: return v.int64Value
        default: return nil
        }
    }
    
    private func parseNxmtpDate(_ dateAdded: String) -> Date? {
        // Format matches `send_to_js/constants.go`:
        // "2006-01-02T15:04:05.000Z"
        // Note: Backend sends local time despite the 'Z' suffix.
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone.current
        formatter.dateFormat = "yyyy-MM-dd'T'HH:mm:ss.SSS'Z'"
        return formatter.date(from: dateAdded)
    }
    
    private struct NxmtpWalkFileInfo: Decodable {
        let size: Int64
        let isFolder: Bool
        let dateAdded: String
        let name: String
        let path: String
        let extension_: String
        let objectId: UInt32
        let sizeUnknown: Bool?
        
        private enum CodingKeys: String, CodingKey {
            case size
            case sizeUnknown
            case isFolder
            case dateAdded
            case name
            case path
            case extension_ = "extension"
            case objectId
        }
    }
    
    private struct TransferSizeInfo: Decodable {
        let total: Int64?
        let sent: Int64?
        let progress: Float?
    }
    // MARK: - Transfer Progress Decoding

    /// Decodes a progress payload, complaining loudly when it cannot.
    ///
    /// A silent `try?` here once cost an entire release: `elapsedTime` was typed
    /// as an integer while the Go layer sent fractional seconds, so *every*
    /// progress update was discarded and the UI sat on "Preparing transfer…"
    /// from the first byte to the last. A decode failure is a bug in the
    /// Swift↔Go contract and must be visible in the log.
    private func decodeFullTransferProgress(from dataAny: Any) -> TransferProgressData? {
        let d = dataFromAny(dataAny)
        guard !d.isEmpty else { return nil }
        do {
            return try JSONDecoder().decode(TransferProgressData.self, from: d)
        } catch {
            if !Self.loggedProgressDecodeFailure {
                Self.loggedProgressDecodeFailure = true
                let raw = String(data: d, encoding: .utf8) ?? "<non-utf8>"
                DebugLog.write("progress decode FAILED: \(error) payload=\(raw)")
            }
            return nil
        }
    }

    /// Logged once per launch: a broken contract repeats thousands of times.
    private static var loggedProgressDecodeFailure = false
    
    private func mapUSBProtocol(from name: String) -> USBProtocol {
        switch name {
        case "USB 2.0": return .usb20
        case "USB 3.0": return .usb30
        case "USB 3.1": return .usb31
        case "USB 3.2": return .usb32
        default: return .unknown
        }
    }
}
