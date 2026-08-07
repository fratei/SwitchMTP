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

/// First-run disclosure explaining what SwitchMTP does to the Mac's USB
/// subsystem in order to talk to a Switch.
///
/// This exists because the behaviour is genuinely surprising: to reach the
/// console, SwitchMTP has to take a USB interface away from a running macOS
/// system daemon. Users deserve to know that before it happens rather than
/// discovering it from a diagnostics dump, so the wording states plainly both
/// what is done and — just as importantly — what is *not* done, since
/// "takes it from a system process" invites the assumption that something is
/// being killed.
struct USBAccessDisclosureView: View {
    let onAcknowledge: () -> Void

    var body: some View {
        ZStack {
            Color.black.opacity(0.25)
                .ignoresSafeArea()

            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 10) {
                    Image(systemName: "cable.connector")
                        .font(.system(size: 20, weight: .medium))
                        .foregroundStyle(.secondary)
                    Text("How SwitchMTP connects to your Switch")
                        .font(.system(size: 15, weight: .semibold))
                }
                .padding(.horizontal, 20)
                .padding(.top, 16)
                .padding(.bottom, 12)

                Divider()
                    .padding(.horizontal, 20)

                VStack(alignment: .leading, spacing: 14) {
                    Text("macOS automatically connects your Switch to its own photo-import service, ptpcamerad, because DBI's file transfer mode looks like a camera. While that service holds the connection, no other app can use it.")
                        .fixedSize(horizontal: false, vertical: true)

                    Text("To get past this, SwitchMTP briefly resets the USB port your Switch is plugged into. macOS releases the connection, and SwitchMTP takes it immediately.")
                        .fixedSize(horizontal: false, vertical: true)

                    VStack(alignment: .leading, spacing: 8) {
                        DisclosureBullet(
                            icon: "checkmark.circle",
                            tint: .green,
                            text: String(localized: "No apps or processes are ever quit or force-quit.")
                        )
                        DisclosureBullet(
                            icon: "checkmark.circle",
                            tint: .green,
                            text: String(localized: "No administrator password and no special permissions are needed.")
                        )
                        DisclosureBullet(
                            icon: "checkmark.circle",
                            tint: .green,
                            text: String(localized: "Only the port your Switch is plugged into is reset. Other USB devices are untouched.")
                        )
                        DisclosureBullet(
                            icon: "checkmark.circle",
                            tint: .green,
                            text: String(localized: "Nothing on your Mac is changed permanently. macOS reconnects its own service when you disconnect.")
                        )
                    }
                    .padding(.top, 2)

                    Text("If a photo-import window appears when you plug in your Switch, you can close it. Full details are in the Troubleshooting guide.")
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .font(.system(size: 12))
                .padding(.horizontal, 20)
                .padding(.vertical, 16)
                .frame(width: 460, alignment: .leading)

                Divider()
                    .padding(.horizontal, 20)

                HStack {
                    Spacer()
                    Button(String(localized: "Got It")) {
                        onAcknowledge()
                    }
                    .keyboardShortcut(.defaultAction)
                }
                .padding(.horizontal, 20)
                .padding(.vertical, 12)
            }
            .frame(width: 460)
            .background(.regularMaterial)
            .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
            .shadow(radius: 24)
        }
    }
}

private struct DisclosureBullet: View {
    let icon: String
    let tint: Color
    let text: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: icon)
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(tint)
            Text(text)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
        }
    }
}
