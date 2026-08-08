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

import SwiftUI

/// The in-window transfer and install-queue panel.
///
/// This deliberately is not a modal sheet. A sheet blocks the sidebar, and the
/// sidebar is where install drop targets live — so with a sheet up it was
/// impossible to queue another title while one was installing, which is exactly
/// the workflow this panel exists to support.
struct TransferBar: View {
    @ObservedObject var manager: MTPManager
    @State private var isQueueExpanded = false
    @State private var now = Date()

    /// Drives the "no progress for a while" notice and the elapsed counter
    /// during phases where the backend has nothing new to report.
    private let tick = Timer.publish(every: 1, on: .main, in: .common).autoconnect()

    private var stats: TransferStatistics? { manager.transferStats }
    private var phase: TransferPhase { stats?.phase ?? .preprocessing }

    /// No stats yet means the backend has not emitted its first payload.
    private var isPreparing: Bool { manager.isTransferActive && stats == nil }

    private var isIndeterminate: Bool { isPreparing || phase.isIndeterminate }

    private var queued: [InstallQueueItem] {
        manager.installQueue.filter { $0.state == .waiting }
    }

    private var finished: [InstallQueueItem] {
        manager.installQueue.filter { $0.state.isFinished }
    }

    /// Seconds since the last progress payload, or nil when none has arrived.
    private var secondsSinceProgress: TimeInterval? {
        guard let last = manager.lastProgressAt else { return nil }
        return now.timeIntervalSince(last)
    }

    /// True when the backend has gone quiet for long enough to be worth saying
    /// so. Installing is expected to be quiet, so it is exempt.
    ///
    /// Two independent signals feed this, because they catch different
    /// failures. The backend's own flag watches whether *bytes* are moving, so
    /// it catches a console that dribbles out one packet every eighty seconds —
    /// which looks like progress here, since a payload does arrive. This view's
    /// timer watches whether *payloads* are arriving, so it still speaks up if
    /// the backend itself stops reporting.
    private var isStalled: Bool {
        guard manager.isTransferActive, phase != .installing else { return false }
        if stats?.isStalled == true { return true }
        guard let since = secondsSinceProgress else {
            return isPreparing
        }
        return since > 15
    }

    private var title: String {
        if let name = stats?.currentFileName, name != "—", !name.isEmpty {
            switch phase {
            case .installing:
                return String(localized: "Installing \(name) on the console…")
            case .completed:
                return String(localized: "Finishing \(name)…")
            default:
                return name
            }
        }
        return String(localized: "Preparing transfer…")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Divider()
            VStack(alignment: .leading, spacing: 6) {
                if manager.isTransferActive {
                    activeSection
                }
                if !queued.isEmpty || !finished.isEmpty {
                    queueSection
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .background(.bar)
        .onReceive(tick) { now = $0 }
    }

    // MARK: - Active transfer

    @ViewBuilder
    private var activeSection: some View {
        HStack(spacing: 10) {
            Image(systemName: phase == .installing ? "arrow.down.app" : "arrow.up.circle")
                .foregroundStyle(phase == .installing ? Color.orange : Color.accentColor)

            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(title)
                        .font(.system(size: 12, weight: .medium))
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Spacer(minLength: 8)
                    Text(stats?.filesProgressString ?? "")
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }

                if isIndeterminate {
                    ProgressView()
                        .progressViewStyle(.linear)
                } else {
                    ProgressView(value: stats?.progressPercentage ?? 0)
                        .progressViewStyle(.linear)
                }

                HStack(spacing: 10) {
                    Text(detailLine)
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                }

                // Suppressed while stalled: the stall label below says the same
                // thing, localised and with a live counter.
                if let note = stats?.note, !(stats?.isStalled ?? false) {
                    Text(note)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                }

                if isStalled {
                    Label(stalledMessage, systemImage: "clock.badge.exclamationmark")
                        .font(.system(size: 11))
                        .foregroundStyle(.orange)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }

            Button(role: .cancel) {
                manager.cancelTransfer()
            } label: {
                Text("Cancel")
                    .font(.system(size: 11))
            }
            .help(String(localized: "Stop the current transfer"))
        }
    }

    private var detailLine: String {
        guard let stats else { return "" }
        switch phase {
        case .installing:
            return String(localized: "Sent \(stats.totalSentSizeString) — waiting for the console")
        case .preprocessing:
            return String(localized: "Reading files…")
        default:
            let pct = stats.progressPercentage.formatted(.percent.precision(.fractionLength(0)))
            return "\(stats.totalSentSizeString)/\(stats.totalSizeString) · \(pct) · \(stats.speedString) · \(stats.remainingTimeString)"
        }
    }

    private var stalledMessage: String {
        if isPreparing {
            return String(localized: "No progress reported yet. The console may still be preparing — DBI shows the real state on screen.")
        }
        // The backend knows the byte counter has stopped, which is a stronger
        // statement than "we have not heard anything lately" and deserves the
        // more specific advice that comes with it.
        if let stats, stats.isStalled {
            let seconds = Int(max(stats.stalledFor, secondsSinceProgress ?? 0))
            return String(localized: "No data accepted for \(seconds)s. Check the console — DBI may be showing an error or waiting for input. Large compressed titles can exhaust memory in applet mode; launching DBI over a running game avoids that.")
        }
        let seconds = Int(secondsSinceProgress ?? 0)
        return String(localized: "No progress for \(seconds)s. The console may be busy; DBI shows the real state on screen.")
    }

    // MARK: - Queue

    @ViewBuilder
    private var queueSection: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Button {
                    withAnimation(.easeInOut(duration: 0.15)) { isQueueExpanded.toggle() }
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: isQueueExpanded ? "chevron.down" : "chevron.right")
                            .font(.system(size: 9, weight: .semibold))
                        Text(queueSummary)
                            .font(.system(size: 11, weight: .medium))
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help(isQueueExpanded
                      ? String(localized: "Hide the list of queued installs", comment: "Tooltip for the button that collapses the install queue")
                      : String(localized: "Show the list of queued installs", comment: "Tooltip for the button that expands the install queue"))

                Spacer(minLength: 0)

                if !queued.isEmpty {
                    Button(String(localized: "Clear Queue")) {
                        manager.clearPendingInstalls()
                    }
                    .font(.system(size: 11))
                    .buttonStyle(.link)
                    .help(String(localized: "Remove every title that has not been sent yet. The install in progress is not affected.", comment: "Tooltip for the Clear Queue button"))
                }
                if !finished.isEmpty {
                    Button(String(localized: "Clear Finished")) {
                        manager.clearFinishedInstalls()
                    }
                    .font(.system(size: 11))
                    .buttonStyle(.link)
                    .help(String(localized: "Remove the titles that have already been sent, failed or were cancelled.", comment: "Tooltip for the Clear Finished button"))
                }
            }

            if isQueueExpanded {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(manager.installQueue) { item in
                        InstallQueueRow(item: item) {
                            manager.removeFromInstallQueue(item.id)
                        }
                    }
                }
                .padding(.leading, 14)
            }
        }
    }

    private var queueSummary: String {
        var parts: [String] = []
        if !queued.isEmpty {
            parts.append(String(localized: "\(queued.count) queued for install"))
        }
        let failures = finished.filter { if case .failed = $0.state { return true } else { return false } }.count
        if failures > 0 {
            parts.append(String(localized: "\(failures) failed"))
        }
        let sent = finished.filter { $0.state == .sent }.count
        if sent > 0 {
            parts.append(String(localized: "\(sent) sent"))
        }
        return parts.isEmpty ? String(localized: "Install queue") : parts.joined(separator: " · ")
    }
}

private struct InstallQueueRow: View {
    let item: InstallQueueItem
    let onRemove: () -> Void

    private var symbol: String {
        switch item.state {
        case .waiting: return "clock"
        case .active: return "arrow.up.circle.fill"
        case .sent: return "checkmark.circle.fill"
        case .failed: return "exclamationmark.triangle.fill"
        case .cancelled: return "slash.circle"
        }
    }

    private var tint: Color {
        switch item.state {
        case .waiting: return .secondary
        case .active: return .accentColor
        case .sent: return .green
        case .failed: return .red
        case .cancelled: return .secondary
        }
    }

    private var detail: String? {
        switch item.state {
        case .failed(let message): return message
        case .sent: return String(localized: "Sent — DBI reports the result on the console.")
        default: return nil
        }
    }

    /// The row shows a middle-truncated filename, an icon whose colour is the
    /// only indication of state, and a failure reason clipped to two lines. The
    /// tooltip is where the whole of each is actually readable.
    private var tooltip: String {
        let status: String
        switch item.state {
        case .waiting: status = String(localized: "Waiting to be sent", comment: "Install queue item status")
        case .active: status = String(localized: "Sending now", comment: "Install queue item status")
        case .sent: status = String(localized: "Sent — DBI reports the result on the console.")
        case .failed(let message): status = message
        case .cancelled: status = String(localized: "Cancelled", comment: "Install queue item status")
        }
        return "\(item.name) — \(status)"
    }

    var body: some View {
        HStack(alignment: .top, spacing: 6) {
            Image(systemName: symbol)
                .font(.system(size: 10))
                .foregroundStyle(tint)
                .frame(width: 12)
            VStack(alignment: .leading, spacing: 1) {
                Text(item.name)
                    .font(.system(size: 11))
                    .lineLimit(1)
                    .truncationMode(.middle)
                if let detail {
                    Text(detail)
                        .font(.system(size: 10))
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            Spacer(minLength: 4)
            if item.state == .waiting {
                Button {
                    onRemove()
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 10))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
                .help(String(localized: "Remove from queue"))
            }
        }
        .help(tooltip)
    }
}
