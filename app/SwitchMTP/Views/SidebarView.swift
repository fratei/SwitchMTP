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

import SwiftUI
import AppKit
import UniformTypeIdentifiers

struct SidebarView: View {
    @ObservedObject var manager: MTPManager
    @Binding var selectedStorage: MTPStorage?
    @Binding var selectedFavoriteID: UUID?
    @ObservedObject var favoritesManager: FavoritesManager
    var onFavoriteSelected: (FavoriteItem) -> Void

    @State private var showFolderNotFoundAlert = false
    @State private var draggingItem: FavoriteItem?
    @State private var installAlertMessage = ""
    @State private var showInstallAlert = false

    var body: some View {
        List {
            if manager.availableDevices.isEmpty {
                noDeviceView
            } else {
                // MARK: – Favorites Section
                Section(String(localized: "Favorites")) {
                    ForEach(favoritesManager.favorites) { item in
                        favoriteRow(item)
                            .onDrag {
                                draggingItem = item
                                return NSItemProvider(object: item.id.uuidString as NSString)
                            }
                            .onDrop(of: [.text], delegate: FavoriteDropDelegate(
                                item: item,
                                draggingItem: $draggingItem,
                                favoritesManager: favoritesManager
                            ))
                    }
                }

                // MARK: – Devices Section
                Section(String(localized: "Devices")) {
                    ForEach(manager.availableDevices) { device in
                        deviceSection(device)
                    }
                }
            }
        }
        .listStyle(.sidebar)
        .frame(minWidth: 200)
        .alert(String(localized: "Folder Not Found"), isPresented: $showFolderNotFoundAlert) {
            Button(String(localized: "OK"), role: .cancel) {}
        } message: {
            Text("The folder does not exist on this device.")
        }
        .alert(String(localized: "Cannot Install"), isPresented: $showInstallAlert) {
            Button(String(localized: "OK"), role: .cancel) {}
        } message: {
            Text(installAlertMessage)
        }
    }

    private func favoriteRow(_ item: FavoriteItem) -> some View {
        let currentPath = manager.currentPath
        let isActiveLocation = (item.path == currentPath)
        let isSelected = selectedFavoriteID == item.id && isActiveLocation
        
        return HStack(spacing: 8) {
            Image(systemName: item.icon)
                .font(.system(size: 14, weight: .medium))
                .frame(width: 20, alignment: .center)
                .foregroundStyle(isSelected ? .primary : .secondary)
            
            VStack(alignment: .leading, spacing: 0) {
                Text(item.displayName)
                    .font(.system(size: 13))
                    .fontWeight(isSelected ? .semibold : .regular)
                
                Text(item.path)
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            
            Spacer()
        }
        .padding(.vertical, 2)
        .padding(.horizontal, 8)
        .contentShape(Rectangle())
        .onTapGesture {
            selectedFavoriteID = item.id
            handleFavoriteTap(item)
        }
        .listRowBackground(
            RoundedRectangle(cornerRadius: 6)
                .fill(isSelected ? Color.accentColor.opacity(0.15) : Color.clear)
                .padding(.horizontal, 4)
        )
        .contextMenu {
            Button(role: .destructive) {
                favoritesManager.removeFavorite(id: item.id)
            } label: {
                Label(String(localized: "Remove from Favorites"), systemImage: "star.slash")
            }
            .disabled(item.isBuiltIn)
            .help(item.isBuiltIn ? String(localized: "Built-in item cannot be removed.") : String(localized: "Remove item from Favorites list."))
        }
    }

    private func handleFavoriteTap(_ item: FavoriteItem) {
        guard manager.connectionState.isConnected else { return }
        guard manager.selectedStorage != nil else { return }
        onFavoriteSelected(item)
    }

    // MARK: – Device Section
    @ViewBuilder
    private func deviceSection(_ info: MTPDeviceInfo) -> some View {
        let isConnected = isConnected(info)
        let isConnecting = isConnecting(info)
        
        Group {
            HStack {
                Label(info.displayName, systemImage: "smartphone")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(isConnected ? .primary : .secondary)
                
                Spacer()
                
                if isConnecting {
                    ProgressView()
                        .controlSize(.small)
                } else if !isConnected {
                    Button {
                        manager.switchDevice(to: info.id)
                    } label: {
                        Image(systemName: "cable.connector.horizontal")
                            .font(.system(size: 18, weight: .bold))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 6)
                            .background(Color.accentColor.opacity(0.1))
                            .clipShape(Capsule())
                    }
                    .buttonStyle(.plain)
                    .help(String(localized: "Connect Device"))
                }
            }
            .padding(.vertical, 8)
            .contentShape(Rectangle())
            
            if isConnected, case .connected(let connectedDevice) = manager.connectionState {
                let groupedStorages = groupedStorages(for: connectedDevice.storages)
                let showsHeaders = groupedStorages.count > 1
                ForEach(StorageGroup.allCases, id: \.self) { group in
                    let storages = connectedDevice.storages.filter { $0.group == group }
                    if !storages.isEmpty {
                        if showsHeaders {
                            Text(group.title)
                                .font(.system(size: 10, weight: .semibold))
                                .foregroundStyle(.secondary)
                                .textCase(.uppercase)
                                .padding(.top, 6)
                                .padding(.leading, 14)
                        }
                        ForEach(storages) { storage in
                            storageRow(storage)
                                .padding(.leading, 12)
                        }
                    }
                }
            }
        }
    }

    private func groupedStorages(for storages: [MTPStorage]) -> [(group: StorageGroup, storages: [MTPStorage])] {
        StorageGroup.allCases.compactMap { group in
            let groupStorages = storages.filter { $0.group == group }
            return groupStorages.isEmpty ? nil : (group, groupStorages)
        }
    }

    private func isConnected(_ info: MTPDeviceInfo) -> Bool {
        if case .connected = manager.connectionState, manager.deviceId == info.id {
            return true
        }
        return false
    }

    private func isConnecting(_ info: MTPDeviceInfo) -> Bool {
        if case .connecting = manager.connectionState, manager.deviceId == info.id {
            return true
        }
        return false
    }

    // MARK: – Storage Row
    @ViewBuilder
    private func storageRow(_ storage: MTPStorage) -> some View {
        if storage.kind.isInstallTarget {
            InstallStorageDropZone(
                storage: storage,
                manager: manager,
                storageColor: storageColor(storage),
                showInvalidInstallAlert: { message in
                    installAlertMessage = message
                    showInstallAlert = true
                }
            )
        } else if storage.capabilities.browse {
            Button {
                selectedStorage = storage
                manager.selectedStorage = storage
                manager.navigationStack = ["/"]
                manager.loadFiles(at: "/")
            } label: {
                storageRowContent(storage)
            }
            .buttonStyle(.plain)
            .listRowBackground(selectedStorage?.id == storage.id ? Color.accentColor.opacity(0.15) : Color.clear)
        } else {
            storageRowContent(storage, captionOverride: String(localized: "Cannot browse this storage"))
                .opacity(0.65)
                .help(String(localized: "This storage is not browsable."))
        }
    }

    private func storageRowContent(_ storage: MTPStorage, captionOverride: String? = nil) -> some View {
        HStack(spacing: 8) {
            Image(systemName: storage.systemImage)
                .foregroundStyle(.secondary)
                .frame(width: 20)
            VStack(alignment: .leading, spacing: 2) {
                Text(storage.name)
                    .font(.system(size: 12, weight: .medium))
                    .lineLimit(1)
                if storage.showsCapacity {
                    ProgressView(value: Double(storage.usedSpace), total: Double(storage.totalSpace))
                        .progressViewStyle(.linear)
                        .tint(storageColor(storage))
                    // One format string rather than "free" + "of" concatenation:
                    // languages that put the quantities in a different order
                    // cannot be served by gluing fragments together.
                    Text("\(storage.displayFreeSpace) free of \(storage.displayTotalSpace)")
                        .font(.system(size: 10))
                        .foregroundStyle(.secondary)
                } else if let captionOverride {
                    Text(captionOverride)
                        .font(.system(size: 10))
                        .foregroundStyle(.secondary)
                } else if storage.virtual {
                    Text(String(localized: "Generated on demand"))
                        .font(.system(size: 10))
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, 4)
    }

    private func storageColor(_ s: MTPStorage) -> Color {
        let ratio = Double(s.usedSpace) / Double(s.totalSpace)
        if ratio > 0.9 { return .red }
        if ratio > 0.75 { return .orange }
        return .accentColor
    }

    // MARK: – No Device
    private var noDeviceView: some View {
        VStack(spacing: 12) {
            Image(systemName: "cable.connector")
                .font(.system(size: 36))
                .foregroundStyle(.quaternary)
            Text("No Device Connected")
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(.secondary)
            Text("Use a USB-C data cable.\nLaunch DBI.\nPress X → Run MTP responder.")
                .font(.system(size: 11))
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 32)
        .listRowBackground(Color.clear)
        //.listRowSeparator(.hidden)
    }
}

private struct InstallStorageDropZone: View {
    let storage: MTPStorage
    @ObservedObject var manager: MTPManager
    let storageColor: Color
    let showInvalidInstallAlert: (String) -> Void

    @State private var isTargeted = false
    @State private var noticeVisible = false
    @State private var noticeMessage = ""

    private let allowedExtensions = MTPManager.installableExtensions

    private var title: String {
        switch storage.kind {
        case .sdInstall:
            return String(localized: "Install to SD")
        case .nandInstall:
            return String(localized: "Install to NAND")
        default:
            return storage.name
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Button {
                chooseInstallFiles()
            } label: {
                HStack(spacing: 8) {
                    Image(systemName: storage.systemImage)
                        .foregroundStyle(storageColor)
                        .frame(width: 20)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(title)
                            .font(.system(size: 12, weight: .medium))
                            .lineLimit(1)
                        Text(String(localized: "Drop NSP, NSZ, XCI or XCZ files here"))
                            .font(.system(size: 10))
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 0)
                }
                .padding(.vertical, 6)
                .padding(.horizontal, 6)
                .background(
                    RoundedRectangle(cornerRadius: 8)
                        .fill(isTargeted ? Color.accentColor.opacity(0.18) : Color.clear)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: 8)
                        .strokeBorder(isTargeted ? Color.accentColor : Color.secondary.opacity(0.25), style: StrokeStyle(lineWidth: 1, dash: [4, 3]))
                )
            }
            .buttonStyle(.plain)
            .help(storage.name)
            .onDrop(of: [.fileURL], isTargeted: $isTargeted, perform: handleDrop(providers:))

            if noticeVisible {
                Text(noticeMessage)
                    .font(.system(size: 10))
                    .foregroundStyle(.secondary)
                    .padding(.leading, 28)
            }
        }
    }

    private func chooseInstallFiles() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.allowsMultipleSelection = true
        panel.allowedContentTypes = allowedExtensions.compactMap { UTType(filenameExtension: $0) }
        panel.prompt = String(localized: "Install")
        if panel.runModal() == .OK {
            acceptInstallFiles(panel.urls)
        }
    }

    private func handleDrop(providers: [NSItemProvider]) -> Bool {
        let group = DispatchGroup()
        var urls: [URL] = []
        let lock = NSLock()
        for provider in providers where provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
            group.enter()
            provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { item, _ in
                defer { group.leave() }
                let url: URL?
                if let data = item as? Data,
                   let value = String(data: data, encoding: .utf8),
                   let fileURL = URL(string: value) {
                    url = fileURL
                } else if let itemURL = item as? URL {
                    url = itemURL
                } else {
                    url = nil
                }
                if let url {
                    lock.lock()
                    urls.append(url)
                    lock.unlock()
                }
            }
        }

        group.notify(queue: .main) {
            acceptInstallFiles(urls)
        }
        return true
    }

    private func acceptInstallFiles(_ urls: [URL]) {
        let outcome = manager.install(fileURLs: urls, to: storage)
        if outcome.isError {
            showInvalidInstallAlert(outcome.message)
            return
        }
        showInstallNotice(outcome.message)
    }

    private func showInstallNotice(_ message: String) {
        noticeMessage = message
        noticeVisible = true
        DispatchQueue.main.asyncAfter(deadline: .now() + 8) {
            noticeVisible = false
        }
    }
}

// MARK: – Drag & Drop Delegate for Favorites Reordering

private struct FavoriteDropDelegate: DropDelegate {
    let item: FavoriteItem
    @Binding var draggingItem: FavoriteItem?
    let favoritesManager: FavoritesManager

    func performDrop(info: DropInfo) -> Bool {
        draggingItem = nil
        return true
    }
    
    func dropEntered(info: DropInfo) {
        guard let dragging = draggingItem,
              dragging.id != item.id,
              let fromIndex = favoritesManager.favorites.firstIndex(where: { $0.id == dragging.id }),
              let toIndex = favoritesManager.favorites.firstIndex(where: { $0.id == item.id })
        else { return }

        if fromIndex != toIndex {
            withAnimation(.spring(response: 0.3, dampingFraction: 0.7)) {
                favoritesManager.favorites.move(fromOffsets: IndexSet(integer: fromIndex), toOffset: toIndex > fromIndex ? toIndex + 1 : toIndex)
            }
        }
    }

    func dropUpdated(info: DropInfo) -> DropProposal? {
        DropProposal(operation: .move)
    }

    func validateDrop(info: DropInfo) -> Bool {
        draggingItem != nil
    }
}
