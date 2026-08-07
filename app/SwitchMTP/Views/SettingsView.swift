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

// Originally: SettingsView.swift — SwiftMTP
// Copyright © Neighbor-Z. All rights reserved.
// Modified for SwitchMTP (fratei/SwitchMTP). AI section removed.
import SwiftUI

struct SettingsView: View {
    @AppStorage("fileListFontSize") private var fileListFontSize: Int = 12
    @AppStorage("doubleClickToOpenFile") private var doubleClickToOpenFile: Bool = true
    @AppStorage("debugLogEnabled") private var debugLogEnabled: Bool = false
    @Environment(\.colorScheme) var colorScheme
    @State private var selectedTab: Int = 0

    private var currentHeight: CGFloat {
        switch selectedTab {
        case 0: return 330
        case 1: return 300
        default: return 220
        }
    }

    var body: some View {
        TabView(selection: $selectedTab) {
            Form {
                VStack(alignment: .leading, spacing: 16) {
                    Picker(String(localized: "Font Size"), selection: $fileListFontSize) {
                        ForEach(10...16, id: \.self) { size in
                            Text("\(size)").tag(size)
                        }
                    }
                    .pickerStyle(.menu)
                    .frame(maxWidth: 300)

                    HStack {
                        Text(String(localized: "List Action"))
                        Toggle(String(localized: "Double-click to open files", comment: "Setting to open files on double click"), isOn: $doubleClickToOpenFile)
                            .help(String(localized: "When enabled, double-clicking a file exports it to a local cache (if not already cached) and opens it with the default application.", comment: "Tooltip for double-click to open file setting"))
                    }

                    Divider()

                    VStack(alignment: .leading, spacing: 6) {
                        Toggle(String(localized: "Write a diagnostic log", comment: "Setting that enables verbose logging to a file"), isOn: $debugLogEnabled)
                            .help(String(localized: "Records transfer activity to a file. Attach it when reporting a failed transfer.", comment: "Tooltip for the diagnostic log setting"))
                        Text(String(localized: "Records connections, transfers and errors to a file so a failed transfer can be diagnosed. Takes effect after you quit and reopen SwitchMTP.", comment: "Explanation of the diagnostic log setting"))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                        Button(String(localized: "Show Log in Finder", comment: "Button that reveals the diagnostic log file")) {
                            revealDebugLog()
                        }
                        .disabled(!debugLogEnabled)
                    }
                }
            }
            .padding(20)
            .tabItem {
                Label(String(localized: "General", comment: "Tab showing general settings"), systemImage: "gear")
            }
            .tag(0)

            Form {
                VStack(alignment: .leading, spacing: 12) {
                    Image(colorScheme == .dark ? "favicon-32x32-dark" : "favicon-32x32")
                    Text("SwitchMTP")
                        .font(.system(size: 20, weight: .semibold))
                    Text(String(localized: "A macOS file manager for Nintendo Switch via DBI MTP responder.", comment: "Settings App Description"))
                        .font(.body)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)

                    HStack(spacing: 16) {
                        Button {
                            if let url = URL(string: "https://github.com/fratei/SwitchMTP") {
                                NSWorkspace.shared.open(url)
                            }
                        } label: {
                            Image(systemName: "chevron.left.forwardslash.chevron.right")
                        }
                        .help("GitHub")
                        .onHover { isHovering in
                            if isHovering { NSCursor.pointingHand.push() } else { NSCursor.pop() }
                        }
                    }
                    .buttonStyle(.borderless)
                    .imageScale(.large)
                    .foregroundStyle(.secondary)

                    Divider()
                        .padding(.vertical, 4)

                    HStack {
                        Text("v\(UpdateChecker.composedAppVersion())")
                            .foregroundStyle(.secondary)
                        Spacer()
                        UpdateButtonRow()
                    }
                }
            }
            .padding(20)
            .tabItem {
                Label(String(localized: "Info & Update", comment: "Tab showing info and update"), systemImage: "info.circle")
            }
            .tag(1)
        }
        .frame(width: 400, height: currentHeight)
        .animation(.spring(duration: 0.3), value: selectedTab)
        .navigationTitle(String(localized: "Settings"))
    }

    /// Reveals the log, or its folder when the toggle was only just enabled and
    /// nothing has been written yet -- selecting a file that does not exist
    /// silently does nothing, which reads as a broken button.
    private func revealDebugLog() {
        let url = DebugLog.fileURL
        if FileManager.default.fileExists(atPath: url.path) {
            NSWorkspace.shared.activateFileViewerSelecting([url])
        } else {
            NSWorkspace.shared.open(url.deletingLastPathComponent())
        }
    }
}

// MARK: - Update Button Row

private struct UpdateButtonRow: View {
    @StateObject private var checker = UpdateChecker.shared

    var body: some View {
        HStack(spacing: 6) {
            Group {
                switch checker.state {
                case .checking:
                    ProgressView()
                        .controlSize(.small)
                case .updateAvailable:
                    Text(String(localized: "Update available", comment: "Update available status"))
                        .foregroundStyle(.green)
                        .font(.callout)
                        .help(String(localized: "Update available"))
                case .upToDate:
                    Text(String(localized: "App is up to date.", comment: "App is up to date status"))
                        .foregroundStyle(.secondary)
                        .font(.callout)
                        .help(String(localized: "App is up to date."))
                case .failed:
                    Text(String(localized: "Check failed", comment: "Update check failed status"))
                        .foregroundStyle(.red)
                        .font(.callout)
                        .help(String(localized: "Check failed"))
                case .idle:
                    EmptyView()
                }
            }

            switch checker.state {
            case .updateAvailable(let version, let url):
                Button(String(format: String(localized: "Download %@", comment: "Download update button"), version)) {
                    NSWorkspace.shared.open(url)
                }
                .id("download-update-button")

            case .checking:
                Button(String(localized: "Check for Updates...", comment: "Settings Update Button")) {}
                    .disabled(true)
                    .id("check-updates-button-checking")

            default:
                Button(String(localized: "Check for Updates...", comment: "Settings Update Button")) {
                    Task { await checker.checkForUpdates() }
                }
                .id("check-updates-button")
            }
        }
    }
}
