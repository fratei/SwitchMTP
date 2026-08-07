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

// Originally: MainView.swift — SwiftMTP
// Copyright © Neighbor-Z. All rights reserved.
// Modified for SwitchMTP (fratei/SwitchMTP). AI features removed.
import SwiftUI
import Foundation
import AppKit

struct MainView: View {
    @StateObject private var manager = MTPManager()
    @StateObject private var favoritesManager = FavoritesManager()
    @State private var selection: Set<MTPFile.ID> = []
    @State private var isShowingNewFolderDialog = false
    @State private var isShowingRenameDialog = false
    @State private var newFolderName = ""
    @State private var newFileName = ""
    @State private var isShowingDeleteConfirmation = false
    @State private var isShowingDeviceInfo = false
    @State private var searchQuery: String = ""
    @State private var unfilteredFiles: [MTPFile]? = nil
    /// The single pending "replace existing items?" confirmation.
    ///
    /// Import and export used to own one `.alert` each, both titled
    /// "Replace and merge the existing items?". SwiftUI could not tell two
    /// same-titled alerts on one view apart: raising the export flag presented
    /// the import alert, whose Replace button guarded on an empty
    /// `pendingImportURLs` and silently did nothing. Exporting from the GUI was
    /// therefore impossible. One alert with one piece of state cannot collide.
    @State private var pendingReplace: PendingReplace?
    @State private var isShowingFolderNotFound = false
    @State private var selectedFavoriteID: UUID? = nil
    @State private var isShowingGoToFolderDialog = false
    @State private var customFolderPath = ""
    @AppStorage("switchDBILargeInstallTipDismissed") private var switchDBILargeInstallTipDismissed = false
    // Shown once, before the first connection attempt, so the user learns what
    // SwitchMTP does to the USB subsystem before it does it rather than after.
    @AppStorage("usbAccessDisclosureAcknowledged") private var usbAccessDisclosureAcknowledged = false
    /// Set when the notice is re-opened from the Help menu after first run.
    @State private var isShowingUSBDisclosure = false

    /// A queued "replace existing items?" confirmation, for either direction.
    enum PendingReplace {
        case importing([URL])
        case exporting([MTPFile], URL)
    }

    var selectedFiles: [MTPFile] {
        manager.sortedFiles.filter { selection.contains($0.id) }
    }

    private var isShowingReplaceConfirmation: Binding<Bool> {
        Binding(
            get: { pendingReplace != nil },
            set: { if !$0 { pendingReplace = nil } }
        )
    }

    private var selectedStorage: MTPStorage? { manager.selectedStorage }
    private var canUploadToSelectedStorage: Bool { selectedStorage?.capabilities.write == true && selectedStorage?.virtual != true }
    private var canCreateFolderInSelectedStorage: Bool { selectedStorage?.capabilities.makeDirectory == true && selectedStorage?.virtual != true }
    private var canRenameInSelectedStorage: Bool { selectedStorage?.capabilities.rename == true && selectedStorage?.virtual != true }
    private var canDeleteInSelectedStorage: Bool { selectedStorage?.capabilities.delete == true && selectedStorage?.virtual != true }

    var deleteDialogTitle: String {
        if selectedFiles.count == 1, let first = selectedFiles.first {
            let template = String(localized: "Delete \"%@\"?")
            return String(format: template, first.name)
        }
        let count = selectedFiles.count
        if count == 1 {
            let template = String(localized: "Delete %d item?")
            return String(format: template, count)
        } else {
            let template = String(localized: "Delete %d items?")
            return String(format: template, count)
        }
    }

    var body: some View {
        ZStack {
            contentView
            if isShowingDeviceInfo, let device = manager.connectionState.device {
                DeviceInfoOverlay(
                    device: device,
                    selectedStorage: manager.selectedStorage,
                    onDismiss: { isShowingDeviceInfo = false }
                )
                .transition(.opacity)
            }
            if !usbAccessDisclosureAcknowledged || isShowingUSBDisclosure {
                USBAccessDisclosureView {
                    usbAccessDisclosureAcknowledged = true
                    isShowingUSBDisclosure = false
                    // No-op if the app is already running; only the genuine
                    // first run reaches an unstarted manager here.
                    manager.start()
                }
                .transition(.opacity)
            }
        }
        .onAppear {
            // Scanning is deferred until the disclosure has been seen, so on
            // every later launch this is what actually starts the app up.
            if usbAccessDisclosureAcknowledged {
                manager.start()
            }
        }
        .animation(.easeInOut(duration: 0.18), value: manager.transferProgress != nil)
        .animation(.easeInOut(duration: 0.18), value: manager.isTransferActive)
        .animation(.easeInOut(duration: 0.18), value: isShowingDeviceInfo)
        .animation(.easeInOut(duration: 0.18), value: usbAccessDisclosureAcknowledged)
        .animation(.easeInOut(duration: 0.18), value: isShowingUSBDisclosure)
        .focusedSceneValue(\.showUSBDisclosureAction, { isShowingUSBDisclosure = true })
        .focusedSceneValue(\.isStarted, manager.isStarted)
        .toolbar { toolbarContent }
        .onChange(of: manager.connectionState) { newState in
            if case .disconnected = newState {
                selection = []
            }
        }
        .onChange(of: searchQuery) { query in
            if query.isEmpty {
                if let original = unfilteredFiles {
                    manager.files = original
                    unfilteredFiles = nil
                }
            } else {
                if unfilteredFiles == nil {
                    unfilteredFiles = manager.files
                }
                manager.files = unfilteredFiles!.filter { $0.name.localizedCaseInsensitiveContains(query) }
            }
        }
        .onChange(of: manager.currentPath) { _ in
            selection = []
            searchQuery = ""
            unfilteredFiles = nil
        }
        .alert("New Folder", isPresented: $isShowingNewFolderDialog) {
            TextField("Folder name", text: $newFolderName)
            Button("Cancel", role: .cancel) {}
            Button("Create") {
                if canCreateFolderInSelectedStorage, !newFolderName.trimmingCharacters(in: .whitespaces).isEmpty {
                    manager.createFolder(named: newFolderName.trimmingCharacters(in: .whitespaces))
                }
                newFolderName = ""
            }
        } message: {
            Text("Enter a name for the new folder.")
        }
        .alert(String(localized: "Rename"), isPresented: $isShowingRenameDialog) {
            TextField(String(localized: "New Name"), text: $newFileName)
            Button(String(localized: "Cancel"), role: .cancel) {}
            Button(String(localized: "Rename")) {
                if canRenameInSelectedStorage, let file = selectedFiles.first, !newFileName.trimmingCharacters(in: .whitespaces).isEmpty {
                    manager.renameFile(file, to: newFileName.trimmingCharacters(in: .whitespaces))
                }
                newFileName = ""
            }
        } message: {
            if let file = selectedFiles.first {
                Text(String(format: String(localized: "Enter a new name for \"%@\"."), file.name))
            }
        }
        .alert(
            String(localized: "Replace and merge the existing items?"),
            isPresented: isShowingReplaceConfirmation,
            presenting: pendingReplace
        ) { pending in
            // `presenting:` hands the action closure the value captured when the
            // alert was raised. Reading `pendingReplace` here instead would
            // always see nil: SwiftUI drives `isPresented` back to false (which
            // clears the state) before it runs the button action.
            Button(String(localized: "Replace")) {
                pendingReplace = nil
                switch pending {
                case .importing(let urls):
                    guard !urls.isEmpty, canUploadToSelectedStorage else { return }
                    manager.upload(sourceURLs: urls)
                case .exporting(let files, let destinationURL):
                    guard !files.isEmpty else { return }
                    manager.download(files: files, destinationURL: destinationURL)
                }
            }
            Button("Cancel", role: .cancel) {
                pendingReplace = nil
            }
        }
        .confirmationDialog(
            deleteDialogTitle,
            isPresented: $isShowingDeleteConfirmation,
            titleVisibility: .visible
        ) {
            Button("Delete", role: .destructive) {
                if canDeleteInSelectedStorage {
                    manager.deleteFiles(selectedFiles)
                }
                selection = []
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This action cannot be undone.")
        }
        .alert(String(localized: "Folder Not Found"), isPresented: $isShowingFolderNotFound) {
            Button(String(localized: "OK"), role: .cancel) {}
        } message: {
            Text("The folder does not exist on this device.")
        }
        .alert(String(localized: "Go to Folder"), isPresented: $isShowingGoToFolderDialog) {
            TextField(String(localized: "/path/to/folder"), text: $customFolderPath)
            Button(String(localized: "Cancel"), role: .cancel) { customFolderPath = "" }
            Button(String(localized: "Go")) {
                let path = customFolderPath.trimmingCharacters(in: .whitespaces)
                if !path.isEmpty {
                    navigateAndHandleError(to: path)
                }
                customFolderPath = ""
            }
        } message: {
            Text(String(localized: "Enter the absolute path on the device."))
        }
        .alert(String(localized: "Item Exists"), isPresented: $manager.isShowingNameConflictAlert) {
            Button(String(localized: "OK"), role: .cancel) {}
        } message: {
            Text(String(localized: "A file or folder with the same name already exists."))
        }
        .onReceive(NotificationCenter.default.publisher(for: NSNotification.Name("SwitchMTPImportAction"))) { notification in
            if let urls = notification.userInfo?["urls"] as? [URL] {
                handleImport(urls)
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: NSNotification.Name("SwitchMTPExportAction"))) { notification in
            if let destinationURL = notification.userInfo?["destinationURL"] as? URL {
                handleExport(destinationURL: destinationURL, files: selectedFiles)
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: NSNotification.Name("SwitchMTPDeleteAction"))) { _ in
            if canDeleteInSelectedStorage, !selectedFiles.isEmpty {
                isShowingDeleteConfirmation = true
            }
        }
        .focusedSceneValue(\.isConnected, manager.connectionState.isConnected)
        .focusedSceneValue(\.canGoBack, manager.canGoBack)
        .focusedSceneValue(\.isTransferActive, manager.isTransferActive)
        .focusedSceneValue(\.isSelectedFilesEmpty, selectedFiles.isEmpty)
        .focusedSceneValue(\.isSingleItemSelected, selectedFiles.count == 1)
        .focusedSceneValue(\.isSingleFileSelected, selectedFiles.count == 1 && !selectedFiles[0].isDirectory)
        .focusedSceneValue(\.navigateToPathAction, { manager.navigateToPath($0) })
        .focusedSceneValue(\.navigateBackAction, { manager.navigateBack() })
        .focusedSceneValue(\.showFolderPromptAction, { isShowingGoToFolderDialog = true })
        .focusedSceneValue(\.showDeviceInfoAction, { isShowingDeviceInfo = true })
        .focusedSceneValue(\.showNewFolderAction, {
            if canCreateFolderInSelectedStorage { isShowingNewFolderDialog = true }
        })
        .focusedSceneValue(\.showRenameAction, {
            if canRenameInSelectedStorage, let first = selectedFiles.first {
                newFileName = first.name
                isShowingRenameDialog = true
            }
        })
        .focusedSceneValue(\.showDeleteConfirmationAction, {
            if canDeleteInSelectedStorage { isShowingDeleteConfirmation = true }
        })
        .focusedSceneValue(\.connectDeviceAction, { manager.connectDevice() })
        .focusedSceneValue(\.disconnectDeviceAction, { manager.disconnectDevice(); manager.selectedStorage = nil })
        .focusedSceneValue(\.openFileAction, {
            NotificationCenter.default.post(name: NSNotification.Name("SwitchMTPOpenFileAction"), object: nil)
        })
        .focusedSceneValue(\.quickLookAction, {
            NotificationCenter.default.post(name: NSNotification.Name("SwitchMTPToggleQuickLook"), object: nil)
        })
        // Scene-scoped, not focus-scoped: `focusedValue` resolves only when the
        // view is in the key-window focus chain, which the file list steals as
        // soon as it appears. That left `\.mtpManager` nil for the Switch menu,
        // disabling every workflow command, and stripped the diagnostics from
        // Help ▸ Report an Issue.
        .focusedSceneValue(\.mtpManager, manager)
        // Derived here rather than in the menu itself: this view observes the
        // manager, so recomputing the snapshot is what makes connection and
        // storage changes reach the Switch menu at all.
        .focusedSceneValue(\.switchMenuState, SwitchMenuState(manager))
    }

    private var contentView: some View {
        Group {
            if #available(macOS 13.0, *) {
                NavigationSplitView {
                    SidebarView(
                        manager: manager,
                        selectedStorage: $manager.selectedStorage,
                        selectedFavoriteID: $selectedFavoriteID,
                        favoritesManager: favoritesManager,
                        onFavoriteSelected: { item in
                            handleFavoriteTap(item)
                        }
                    )
                    .navigationSplitViewColumnWidth(min: 200, ideal: 230, max: 280)
                } detail: {
                    fileBrowserView
                        .searchable(text: $searchQuery, placement: .toolbar, prompt: Text("Search"))
                        .safeAreaInset(edge: .top, spacing: 0) {
                            if manager.connectionState.isConnected {
                                pathBar
                            }
                        }
                        .safeAreaInset(edge: .bottom, spacing: 0) {
                            VStack(spacing: 0) {
                                if showsTransferBar {
                                    TransferBar(manager: manager)
                                }
                                statusBar
                            }
                        }
                }
            } else {
                NavigationView {
                    SidebarView(
                        manager: manager,
                        selectedStorage: $manager.selectedStorage,
                        selectedFavoriteID: $selectedFavoriteID,
                        favoritesManager: favoritesManager,
                        onFavoriteSelected: { item in
                            handleFavoriteTap(item)
                        }
                    )
                    .frame(minWidth: 200, idealWidth: 230, maxWidth: 280)

                    VStack(spacing: 0) {
                        if manager.connectionState.isConnected {
                            pathBar
                        }
                        fileBrowserView
                            .searchable(text: $searchQuery, placement: .toolbar, prompt: Text("Search"))
                        if showsTransferBar {
                            TransferBar(manager: manager)
                        }
                        statusBar
                    }
                }
            }
        }
    }

    /// The transfer bar stays up while the install queue still has work, so the
    /// gap between one title finishing and the next starting does not flicker
    /// the whole panel out of existence.
    private var showsTransferBar: Bool {
        manager.isTransferActive || !manager.installQueue.isEmpty
    }

    @ViewBuilder
    private var fileBrowserView: some View {
        VStack(spacing: 0) {
            bannerArea
            if manager.connectionState.isConnected {
                FileListView(
                    manager: manager,
                    selection: $selection,
                    onDoubleClick: { file in
                        if file.isDirectory { manager.navigate(to: file) }
                    },
                    onAddToFavorites: { file in
                        let fullPath = file.path
                        favoritesManager.addFavorite(name: file.name, path: fullPath)
                    },
                    isPathFavorited: { path in
                        favoritesManager.contains(path: path)
                    }
                )
            } else {
                noDeviceChecklist
            }
        }
    }

    @ViewBuilder
    private var bannerArea: some View {
        if manager.pendingNavigationPath != nil && showsTransferBar {
            SwitchMTPBanner(
                title: manager.showingCachedListing
                    ? String(localized: "Showing this folder as it was last seen")
                    : String(localized: "This folder opens when the transfer finishes"),
                message: manager.showingCachedListing
                    ? String(localized: "The console can only do one thing at a time over USB, so it cannot check this folder while it is copying. This is the last view SwitchMTP has of it, and it refreshes as soon as the transfer finishes.")
                    : String(localized: "The console can only do one thing at a time over USB, so it cannot list a folder while it is copying. SwitchMTP will open this folder as soon as the transfer is done."),
                systemImage: "clock",
                tint: .accentColor
            )
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        } else if let device = manager.connectionState.device, device.profile.needsModeChange {
            SwitchMTPBanner(
                title: String(localized: "Switch is in a homebrew USB mode"),
                message: device.advice.isEmpty
                    ? String(localized: "SwitchMTP needs DBI's MTP responder. On the Switch, open DBI, press X, and choose “Run MTP responder”. Awoo-Installer and GoldLeaf also appear this way — exit them and start DBI instead.")
                    : device.advice,
                systemImage: "exclamationmark.triangle.fill",
                tint: .orange
            )
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        } else if let device = manager.connectionState.device,
                  device.profile == .switchDBI,
                  !switchDBILargeInstallTipDismissed {
            SwitchMTPBanner(
                title: String(localized: "Tip for large installs"),
                message: String(localized: "For large installs, launch DBI in title mode (hold R while starting a game). In applet mode large transfers fail with \"Extra buffers exceeded\"."),
                systemImage: "lightbulb",
                tint: .accentColor,
                onDismiss: { switchDBILargeInstallTipDismissed = true }
            )
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
    }

    private var noDeviceChecklist: some View {
        VStack(spacing: 14) {
            Image(systemName: "cable.connector")
                .font(.system(size: 46))
                .foregroundStyle(.quaternary)
            Text(String(localized: "No Device Connected"))
                .font(.system(size: 16, weight: .semibold))
            VStack(alignment: .leading, spacing: 8) {
                Label(String(localized: "Use a USB-C data cable."), systemImage: "checkmark.circle")
                Label(String(localized: "Launch DBI on the Switch."), systemImage: "checkmark.circle")
                Label(String(localized: "Press X → Run MTP responder."), systemImage: "checkmark.circle")
                Label(String(localized: "If it still fails, run Troubleshooting."), systemImage: "wrench.and.screwdriver")
            }
            .font(.system(size: 13))
            .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // MARK: – Path Bar
    private var pathBar: some View {
        HStack(spacing: 6) {
            Button {
                manager.navigateBack()
            } label: {
                Image(systemName: "chevron.backward")
                    .font(.system(size: 11, weight: .semibold))
            }
            .buttonStyle(.plain)
            .help("Enclosing Folder")
            .onHover { isHovering in
                if isHovering {
                    NSCursor.pointingHand.push()
                } else {
                    NSCursor.pop()
                }
            }
            .disabled(!manager.canGoBack)

            PathBarView(navigationStack: manager.navigationStack) { index in
                manager.navigate(toIndex: index)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(
            Group {
                if #available(macOS 13.0, *) {
                    Color.clear.background(.bar)
                } else {
                    Color(nsColor: .windowBackgroundColor)
                }
            }
        )
        .overlay(alignment: .bottom) {
            Divider()
        }
    }

    // MARK: – Status Bar
    private var statusBar: some View {
        VStack(spacing: 0) {
            Divider()
            HStack {
                Text(statusText)
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                Spacer()
                if let stats = manager.silentTransferStats {
                    HStack(spacing: 8) {
                        ProgressView(value: stats.progressPercentage)
                            .progressViewStyle(.linear)
                            .frame(width: 100)
                        Text(stats.speedString)
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundStyle(.secondary)
                        Text(stats.remainingTimeString)
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundStyle(.secondary)
                        Text(stats.progressPercentage.formatted(.percent.precision(.fractionLength(0))))
                            .font(.system(size: 11, design: .monospaced))
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 5)
        }
        .background(
            Group {
                if #available(macOS 13.0, *) {
                    Color.clear.background(.bar)
                } else {
                    Color(nsColor: .windowBackgroundColor)
                }
            }
        )
    }

    private var statusText: String {
        if let _ = manager.silentTransferStats {
            return String(localized: "Preparing preview...")
        }
        if manager.isTransferActive {
            let stats = manager.transferStats
            let filesProgressString = stats?.filesProgressString ?? ""
            return String(localized: "Transferring…") + " " + filesProgressString
        }
        if manager.isLoading { return String(localized: "Loading…") }
        if case let .error(message) = manager.connectionState { return message }

        if !selection.isEmpty {
            let count = selection.count
            if count == 1 {
                let template = String(localized: "%d item selected")
                return String(format: template, count)
            } else {
                let template = String(localized: "%d items selected")
                return String(format: template, count)
            }
        }

        let count = manager.sortedFiles.count
        if count == 1 {
            let template = String(localized: "%d item")
            return String(format: template, count)
        } else {
            let template = String(localized: "%d items")
            return String(format: template, count)
        }
    }

    // MARK: – Toolbar
    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItemGroup(placement: .navigation) {
            if #unavailable(macOS 13.0) {
                Button {
                    NSApp.keyWindow?.firstResponder?.tryToPerform(#selector(NSSplitViewController.toggleSidebar(_:)), with: nil)
                } label: {
                    Label(String(localized: "Toggle Sidebar"), systemImage: "sidebar.leading")
                }
                .help(String(localized: "Toggle Sidebar"))
            }

            if manager.connectionState.isConnected {
                Button {
                    manager.disconnectDevice()
                    manager.selectedStorage = nil
                } label: {
                    Label("Disconnect", systemImage: "cable.connector.slash")
                }
                .help("Disconnect Device")
            } else {
                Button {
                    manager.connectDevice()
                } label: {
                    Label("Connect Device", systemImage: "cable.connector")
                }
                .help(toolbarHelp(String(localized: "Connect Device"), [
                    (!manager.isStarted, String(localized: "Review the USB access notice first.", comment: "Reason the Connect button is unavailable on first run")),
                ]))
                // The toolbar sits in the titlebar, which the first-run
                // disclosure overlay cannot cover, so it must disable itself.
                .disabled(!manager.isStarted)
            }
        }

        ToolbarItemGroup(placement: .primaryAction) {
            Button {
                let panel = NSOpenPanel()
                panel.canChooseFiles = true
                panel.canChooseDirectories = true
                panel.allowsMultipleSelection = true
                panel.prompt = String(localized: "Import")
                if panel.runModal() == .OK {
                    handleImport(panel.urls)
                }
            } label: {
                if #available(macOS 13.0, *) {
                    Label(String(localized: "Import"), systemImage: "iphone.and.arrow.forward.inward")
                } else {
                    Label(String(localized: "Import"), systemImage: "arrow.down.to.line")
                }
            }
            .help(toolbarHelp(String(localized: "Import files from Mac to device"), [
                (!manager.connectionState.isConnected, notConnectedReason),
                (manager.isTransferActive, transferBusyReason),
                (!canUploadToSelectedStorage, String(localized: "This storage does not allow uploads.")),
            ]))
            .disabled(!manager.connectionState.isConnected || manager.isTransferActive || !canUploadToSelectedStorage)

            Button {
                let panel = NSOpenPanel()
                panel.canChooseFiles = false
                panel.canChooseDirectories = true
                panel.prompt = String(localized: "Export Here")
                if panel.runModal() == .OK, let url = panel.url {
                    handleExport(destinationURL: url, files: selectedFiles)
                }
            } label: {
                if #available(macOS 13.0, *) {
                    Label(String(localized: "Export"), systemImage: "iphone.and.arrow.forward.outward")
                } else {
                    Label(String(localized: "Export"), systemImage: "arrow.up.to.line")
                }
            }
            .help(toolbarHelp(String(localized: "Export selected files to Mac"), [
                (!manager.connectionState.isConnected, notConnectedReason),
                (manager.isTransferActive, transferBusyReason),
                (selectedFiles.isEmpty, selectSomethingReason),
            ]))
            .disabled(selectedFiles.isEmpty || !manager.connectionState.isConnected || manager.isTransferActive)

            Spacer(minLength: 12)

            Button {
                if canCreateFolderInSelectedStorage {
                    isShowingNewFolderDialog = true
                }
            } label: {
                Label("New Folder", systemImage: "folder.badge.plus")
            }
            .help(toolbarHelp(String(localized: "Create new folder"), [
                (!manager.connectionState.isConnected, notConnectedReason),
                (manager.isTransferActive, transferBusyReason),
                (!canCreateFolderInSelectedStorage, String(localized: "This storage does not allow creating folders.")),
            ]))
            .disabled(!manager.connectionState.isConnected || manager.isTransferActive || !canCreateFolderInSelectedStorage)

            Button {
                if canRenameInSelectedStorage, let first = selectedFiles.first {
                    newFileName = first.name
                    isShowingRenameDialog = true
                }
            } label: {
                Label("Rename", systemImage: "character.cursor.ibeam")
            }
            .help(toolbarHelp(String(localized: "Rename"), [
                (!manager.connectionState.isConnected, notConnectedReason),
                (manager.isTransferActive, transferBusyReason),
                (selectedFiles.count != 1, String(localized: "Select exactly one item first.", comment: "Reason a toolbar button is unavailable")),
                (!canRenameInSelectedStorage, String(localized: "This storage does not allow renaming items.")),
            ]))
            .disabled(!manager.connectionState.isConnected || manager.isTransferActive || selectedFiles.count != 1 || !canRenameInSelectedStorage)

            Button {
                if canDeleteInSelectedStorage {
                    isShowingDeleteConfirmation = true
                }
            } label: {
                Label("Delete", systemImage: "trash")
            }
            .help(toolbarHelp(String(localized: "Delete selected items"), [
                (!manager.connectionState.isConnected, notConnectedReason),
                (manager.isTransferActive, transferBusyReason),
                (selectedFiles.isEmpty, selectSomethingReason),
                (!canDeleteInSelectedStorage, String(localized: "This storage does not allow deleting items.")),
            ]))
            .disabled(selectedFiles.isEmpty || !manager.connectionState.isConnected || manager.isTransferActive || !canDeleteInSelectedStorage)

            Spacer(minLength: 12)

            Button {
                isShowingDeviceInfo = true
            } label: {
                Label(String(localized: "Device Info"), systemImage: "info.circle")
            }
            .help(toolbarHelp(String(localized: "Show device information"), [
                (!manager.connectionState.isConnected, notConnectedReason),
                (manager.isTransferActive, transferBusyReason),
            ]))
            .disabled(!manager.connectionState.isConnected || manager.isTransferActive)
        }
    }

    // MARK: – Toolbar tooltips

    /// A greyed-out button tells the user *that* something is unavailable but
    /// never *why*, and the reasons here are not interchangeable: wrong
    /// storage, nothing selected, no device and "busy" each need a different
    /// response. Toolbar items keep their tooltip while disabled, so this is
    /// the one place that explanation fits.
    ///
    /// The reason replaces the description rather than being appended to it.
    /// The button's icon and its menu-bar twin already say what it does, the
    /// blocked case is the only one carrying new information, and a tooltip
    /// containing a newline does not render at all here.
    ///
    /// Blockers are listed in the order they should be reported; the first one
    /// that applies wins.
    private func toolbarHelp(_ action: String, _ blockers: [(Bool, String)]) -> String {
        blockers.first(where: { $0.0 })?.1 ?? action
    }

    private var notConnectedReason: String {
        String(localized: "Connect a device first.", comment: "Reason a toolbar button is unavailable")
    }

    private var transferBusyReason: String {
        String(localized: "Wait for the current transfer to finish.", comment: "Reason a toolbar button is unavailable")
    }

    private var selectSomethingReason: String {
        String(localized: "Select one or more items first.", comment: "Reason a toolbar button is unavailable")
    }

    private func handleImport(_ urls: [URL]) {
        guard !urls.isEmpty else { return }
        guard canUploadToSelectedStorage else { return }
        if manager.hasConflictingItems(for: urls) {
            pendingReplace = .importing(urls)
            return
        }

        manager.upload(sourceURLs: urls)
    }

    private func handleExport(destinationURL: URL, files: [MTPFile]) {
        guard !files.isEmpty else { return }
        DebugLog.write("handleExport dest=\(destinationURL.path) files=\(files.map(\.name))")
        if hasExportConflicts(files: files, destinationURL: destinationURL) {
            pendingReplace = .exporting(files, destinationURL)
            return
        }
        manager.download(files: files, destinationURL: destinationURL)
    }

    private func hasExportConflicts(files: [MTPFile], destinationURL: URL) -> Bool {
        for file in files {
            let targetURL = destinationURL.appendingPathComponent(file.name)
            if FileManager.default.fileExists(atPath: targetURL.path) {
                DebugLog.write("export conflict at \(targetURL.path)")
                return true
            }
        }
        return false
    }

    private func navigateAndHandleError(to path: String) {
        guard manager.connectionState.isConnected else { return }
        guard manager.selectedStorage != nil else { return }

        // A transfer owns the USB session, so the navigation is deferred rather
        // than attempted and nothing can have failed. Falling through would let
        // the recovery below dispose a perfectly healthy session and abort the
        // copy.
        guard !manager.isTransferInFlight else {
            manager.navigateToPath(path)
            return
        }

        let previousPath = manager.currentPath

        // Only an error raised by *this* navigation should count as the folder
        // being missing. A message the user simply had not dismissed yet used to
        // be enough to tear down and rebuild the connection.
        manager.errorMessage = nil

        manager.navigateToPath(path)

        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
            // A transfer that started in the meantime is not evidence of a bad
            // path either, and disposing under it would kill the copy.
            guard !manager.isTransferInFlight else { return }
            let hasError = manager.errorMessage != nil || (manager.connectionState.device == nil)
            var isErrorState = false
            if case .error = manager.connectionState {
                isErrorState = true
            }
            if hasError || isErrorState {
                manager.reconnectAndRestore(to: previousPath)
                manager.errorMessage = nil
                isShowingFolderNotFound = true
            }
        }
    }

    private func handleFavoriteTap(_ item: FavoriteItem) {
        navigateAndHandleError(to: item.path)
    }
}

private struct SwitchMTPBanner: View {
    let title: String
    let message: String
    let systemImage: String
    let tint: Color
    var onDismiss: (() -> Void)? = nil

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: systemImage)
                .font(.system(size: 16, weight: .semibold))
                .foregroundStyle(tint)
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.system(size: 13, weight: .semibold))
                Text(message)
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if let onDismiss {
                Button(action: onDismiss) {
                    Image(systemName: "xmark")
                        .font(.system(size: 11, weight: .semibold))
                }
                .buttonStyle(.plain)
                .help(String(localized: "Dismiss"))
            }
        }
        .padding(10)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(tint.opacity(0.12))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .strokeBorder(tint.opacity(0.28))
        )
    }
}

// MARK: – Device Info Overlay
private struct DeviceInfoOverlay: View {
    let device: MTPDevice
    let selectedStorage: MTPStorage?
    let onDismiss: () -> Void

    var body: some View {
        ZStack {
            Color.black.opacity(0.25)
                .ignoresSafeArea()
                .onTapGesture { onDismiss() }

            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Image(systemName: "smartphone")
                        .font(.system(size: 20, weight: .medium))
                        .foregroundStyle(.secondary)
                    Text("Device Information")
                        .font(.system(size: 15, weight: .semibold))
                }
                .padding(.horizontal, 20)
                .padding(.top, 16)
                .padding(.bottom, 12)

                Divider()
                    .padding(.horizontal, 20)

                VStack(alignment: .leading, spacing: 0) {
                    InfoRow(label: String(localized: "Manufacturer"), value: device.manufacturer.isEmpty ? "-" : device.manufacturer)
                    InfoRow(label: String(localized: "Device Model"), value: device.name)
                    InfoRow(label: String(localized: "Protocol"), value: device.usbLinkDescription)
                    if let storage = selectedStorage {
                        InfoRow(label: String(localized: "Storage"), value: storage.name)
                        InfoRow(label: String(localized: "Free Space"), value: storage.displayFreeSpace)
                        InfoRow(label: String(localized: "Total Space"), value: storage.displayTotalSpace)
                    }
                }
                .padding(.vertical, 6)

                Divider()
                    .padding(.horizontal, 20)

                HStack {
                    Spacer()
                    Button(String(localized: "Close")) {
                        onDismiss()
                    }
                    .keyboardShortcut(.escape, modifiers: [])
                    Spacer()
                }
                .padding(.horizontal, 20)
                .padding(.vertical, 12)
            }
            .frame(width: 360)
            .background(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(Color(nsColor: .windowBackgroundColor))
                    .shadow(radius: 16, y: 4)
            )
        }
    }
}

private struct InfoRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(label)
                .font(.system(size: 12))
                .foregroundStyle(.secondary)
                .frame(width: 110, alignment: .trailing)
            Text(value)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(.primary)
                .textSelection(.enabled)
            Spacer()
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 8)
    }
}

