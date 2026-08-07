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

enum QuickLookOverlayState: Equatable {
    case folder(MTPFile)
    case prompt(MTPFile)
    case loading
    
    static func == (lhs: QuickLookOverlayState, rhs: QuickLookOverlayState) -> Bool {
        switch (lhs, rhs) {
        case (.folder(let a), .folder(let b)), (.prompt(let a), .prompt(let b)):
            return a.id == b.id
        case (.loading, .loading):
            return true
        default:
            return false
        }
    }
}

struct QuickLookOverlayView: View {
    let state: QuickLookOverlayState
    let onLoadPreview: (() -> Void)?
    
    var body: some View {
        VStack(spacing: 12) {
            switch state {
            case .folder(let file):
                fileInfoView(for: file)
            case .prompt(let file):
                fileInfoView(for: file)
            case .loading:
                ProgressView()
                    .controlSize(.large)
                    .padding(.bottom, 8)
                Text(String(localized: "Preparing preview..."))
                    .font(.body)
                    .foregroundColor(.secondary)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        // A NOT translucent background mimicking Quick Look standard background
        .background(Color(NSColor.windowBackgroundColor).opacity(1))
    }
    
    @ViewBuilder
    private func fileInfoView(for file: MTPFile) -> some View {
        HStack(spacing: 20) {
            Image(nsImage: getThumbnailIcon(for: file))
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(width: 160, height: 160)
                .shadow(color: Color.black.opacity(0.15), radius: 8, x: 0, y: 4)
            
            VStack(alignment: .leading, spacing: 6) {
                Text(file.name)
                    .font(.system(size: 24, weight: .regular))
                    .multilineTextAlignment(.leading)
                
                VStack(alignment: .leading, spacing: 4) {
                    if file.isDirectory {
                        Text(file.kind)
                    } else {
                        Text("\(file.displaySize) - \(file.kind)")
                    }
                    
                    let df = DateFormatter()
                    let _ = df.dateStyle = .medium
                    let _ = df.timeStyle = .short
                    Text(file.dateModified.map { df.string(from: $0) } ?? String(localized: "Date not reported by the device"))
                    
                    if case .prompt = state {
                        if !file.isDirectory {
                            Button(String(localized: "Load Preview")) {
                                onLoadPreview?()
                            }
                            .buttonStyle(.borderedProminent)
                            .controlSize(.large)
                            .padding(.top, 5)
                            
                            Text(String(localized: "Loading a large file preview may take some time and cannot be canceled."))
                            .font(.system(size: 11))
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                            .padding(.top, 4)
                        }
                    }
                }
                .font(.system(size: 13))
                .foregroundColor(.secondary)
            }
        }
    }
    
    private func getThumbnailIcon(for file: MTPFile) -> NSImage {
        let iconSize = NSSize(width: 256, height: 256)
        if file.isDirectory {
            let icon = NSWorkspace.shared.icon(for: .folder)
            icon.size = iconSize
            return icon
        }
        let ext = file.extension_.lowercased()
        if !ext.isEmpty {
            if let utType = UTType(filenameExtension: ext),
               let icon = NSWorkspace.shared.icon(for: utType) as NSImage? {
                icon.size = iconSize
                return icon
            }
        }
        let genericIcon = NSWorkspace.shared.icon(for: .data)
        genericIcon.size = iconSize
        return genericIcon
    }
}
