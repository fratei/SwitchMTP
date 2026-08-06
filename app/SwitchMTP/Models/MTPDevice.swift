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

/// Which kind of device we are talking to. `homebrewUSB` is the trap case:
/// 0x057E:0x3000 is libnx's generic homebrew USB product ID, presented by DBI's
/// "DBIbackend" mode, Awoo-Installer and GoldLeaf alike. All of them speak
/// their own non-MTP protocols, so the device is detectable but not usable —
/// the UI tells the user how to get to DBI's MTP responder instead.
enum DeviceProfile: String, Equatable, Sendable {
    case generic
    case switchDBI
    case switchHOS
    case homebrewUSB

    init(backendValue: String?) {
        self = DeviceProfile(rawValue: backendValue ?? "") ?? .generic
    }

    var isSwitch: Bool { self == .switchDBI || self == .switchHOS || self == .homebrewUSB }

    /// True when the device cannot serve MTP at all and needs user action.
    var needsModeChange: Bool { self == .homebrewUSB }
}

struct MTPDevice: Identifiable, Equatable {
    let id: String
    let name: String
    let manufacturer: String
    let storages: [MTPStorage]
    let usbProtocol: USBProtocol
    let usbSpeedMbps: Int
    let maxSpeedBytesPerSecond: Double
    var profile: DeviceProfile = .generic
    /// Backend-supplied guidance shown when the device is detected but not usable.
    var advice: String = ""

    var usbLinkDescription: String {
        usbProtocol.linkDescription(speedMbps: usbSpeedMbps)
    }

    static let mock = MTPDevice(
        id: "device-1",
        name: "Nintendo Switch (DBI)",
        manufacturer: "Nintendo",
        storages: [
            MTPStorage(id: "storage-1", name: "SD Card", freeSpace: 45_000_000_000,
                       totalSpace: 256_000_000_000, kind: .sdCard, capabilities: .full)
        ],
        usbProtocol: .usb31,
        usbSpeedMbps: 10_000,
        maxSpeedBytesPerSecond: 880_000_000,
        profile: .switchDBI
    )
}

/// What a storage actually is. This matters on DBI far more than on a phone:
/// two of its storages are write-only install triggers that must never be
/// browsed, and one is a virtual filesystem of dumps generated on demand.
enum StorageKind: String, Equatable, Sendable {
    case sdCard
    case nandUser
    case nandSystem
    case installedGames
    case sdInstall
    case nandInstall
    case saves
    case album
    case gamecard
    case custom
    case unknown

    init(backendValue: String?) {
        self = StorageKind(rawValue: backendValue ?? "") ?? .unknown
    }

    /// Install storages are drop targets, not folders -- DBI starts installing
    /// the moment a file lands in one, so presenting them as browsable
    /// directories invites the user to double-click into a dead end.
    var isInstallTarget: Bool { self == .sdInstall || self == .nandInstall }

    var systemImage: String {
        switch self {
        case .sdCard: return "sdcard.fill"
        case .nandUser, .nandSystem: return "internaldrive.fill"
        case .installedGames: return "gamecontroller.fill"
        case .sdInstall, .nandInstall: return "arrow.down.circle.fill"
        case .saves: return "square.and.arrow.down.on.square.fill"
        case .album: return "photo.on.rectangle.angled"
        case .gamecard: return "opticaldiscdrive.fill"
        case .custom: return "folder.fill"
        case .unknown: return "externaldrive.fill"
        }
    }

    /// Sidebar grouping. Install targets are separated so they read as actions.
    var group: StorageGroup {
        switch self {
        case .sdInstall, .nandInstall: return .install
        case .installedGames, .gamecard: return .dumps
        case .nandUser, .nandSystem: return .system
        default: return .storage
        }
    }
}

enum StorageGroup: Int, Comparable, CaseIterable {
    case storage = 0
    case install = 1
    case dumps = 2
    case system = 3

    var title: String {
        switch self {
        case .storage: return String(localized: "Storage")
        case .install: return String(localized: "Install")
        case .dumps: return String(localized: "Dumps")
        case .system: return String(localized: "System")
        }
    }

    static func < (lhs: StorageGroup, rhs: StorageGroup) -> Bool {
        lhs.rawValue < rhs.rawValue
    }
}

/// What the UI may offer for a storage. The backend derives this from DBI's
/// declared AccessCapability combined with what the storage actually is, so
/// the UI can grey out actions instead of letting them fail on the device.
struct StorageCapabilities: Equatable, Sendable {
    var browse = true
    var read = true
    var write = false
    var delete = false
    var rename = false
    var makeDirectory = false
    var installTarget = false

    static let readOnly = StorageCapabilities()
    static let full = StorageCapabilities(
        browse: true, read: true, write: true,
        delete: true, rename: true, makeDirectory: true, installTarget: false
    )

    init(browse: Bool = true, read: Bool = true, write: Bool = false,
         delete: Bool = false, rename: Bool = false,
         makeDirectory: Bool = false, installTarget: Bool = false) {
        self.browse = browse
        self.read = read
        self.write = write
        self.delete = delete
        self.rename = rename
        self.makeDirectory = makeDirectory
        self.installTarget = installTarget
    }

    init(json: [String: Any]?) {
        func flag(_ key: String, _ fallback: Bool) -> Bool {
            (json?[key] as? Bool) ?? fallback
        }
        self.init(
            browse: flag("browse", true),
            read: flag("read", true),
            write: flag("write", false),
            delete: flag("delete", false),
            rename: flag("rename", false),
            makeDirectory: flag("makeDirectory", false),
            installTarget: flag("installTarget", false)
        )
    }
}

struct MTPStorage: Identifiable, Equatable {
    let id: String
    let name: String
    let freeSpace: Int64
    let totalSpace: Int64

    var kind: StorageKind = .unknown
    var capabilities: StorageCapabilities = .full
    /// Virtual storages generate their contents on demand, so their reported
    /// capacity is meaningless and their entries cannot be modified.
    var virtual: Bool = false
    /// False when the device reports a capacity we should not display.
    var sizeReliable: Bool = true
    var order: Int = 0

    var usedSpace: Int64 { totalSpace - freeSpace }

    var displayFreeSpace: String { formatBytes(freeSpace) }
    var displayTotalSpace: String { formatBytes(totalSpace) }

    var systemImage: String { kind.systemImage }
    var group: StorageGroup { kind.group }

    /// Install targets are never listed, so a capacity bar would be a lie.
    var showsCapacity: Bool {
        sizeReliable && !virtual && !kind.isInstallTarget && totalSpace > 0
    }

    private func formatBytes(_ bytes: Int64) -> String {
        let d = Double(bytes)
        if d >= 1e9 { return String(format: "%.1f GB", d / 1e9) }
        if d >= 1e6 { return String(format: "%.1f MB", d / 1e6) }
        return String(format: "%.1f KB", d / 1e3)
    }
}

enum ConnectionState: Equatable {
    case disconnected
    case connecting
    case connected(MTPDevice)
    case error(String)

    var isConnected: Bool {
        if case .connected = self { return true }
        return false
    }

    var device: MTPDevice? {
        if case .connected(let d) = self { return d }
        return nil
    }
}
