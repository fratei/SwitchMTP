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

struct PathBarView: View {
    let navigationStack: [String]
    let onNavigate: (Int) -> Void

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 2) {
                ForEach(Array(navigationStack.enumerated()), id: \.offset) { index, path in
                    if index > 0 {
                        Image(systemName: "chevron.forward")
                            .font(.system(size: 10, weight: .semibold))
                            .foregroundStyle(.tertiary)
                    }
                    Button {
                        onNavigate(index)
                    } label: {
                        Text(
                            index == 0
                                ? String(localized: "Device")
                                : (path as NSString).lastPathComponent
                        )
                            .font(.system(size: 12, weight: index == navigationStack.count - 1 ? .semibold : .regular))
                            .foregroundStyle(index == navigationStack.count - 1 ? .primary : .secondary)
                    }
                    .buttonStyle(.plain)
                    .onHover { isHovering in
                        if isHovering {
                            NSCursor.pointingHand.push()
                        } else {
                            NSCursor.pop()
                        }
                    }
                }
            }
            .padding(.horizontal, 8)
        }
        .frame(height: 22)
    }
}

struct ToolbarActionButton: View {
    let title: String
    let systemImage: String
    let action: () -> Void
    var isDestructive: Bool = false

    var body: some View {
        Button(action: action) {
            Label(title, systemImage: systemImage)
        }
        .help(title)
        .foregroundStyle(isDestructive ? .red : .primary)
    }
}
