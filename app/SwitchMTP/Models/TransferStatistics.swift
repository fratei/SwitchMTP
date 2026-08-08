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

/// Complete transfer progress data structure, corresponding to Go layer
///
/// The field types here are load-bearing: `JSONDecoder` fails the *whole*
/// payload if a single one mismatches, and a dropped progress payload leaves
/// the UI stuck on "Preparing transfer…" for the entire transfer. `elapsedTime`
/// and `speed` in particular are fractional in the Go layer, not integers.
struct TransferProgressData: Decodable {
    let fullPath: String?
    let name: String?
    let elapsedTime: Double?             // seconds, fractional
    let speed: Double?                   // bytes per second
    let totalFiles: Int64?
    let totalDirectories: Int64?
    let filesSent: Int64?
    let filesSentProgress: Double?       // percent, 0-100
    let activeFileSize: TransferSizeInfo?
    let bulkFileSize: TransferSizeInfo?
    let status: String?                  // transfer status
    let note: String?                    // human-readable phase detail
    let indefinite: Bool?                // total size not known in advance
    let currentFile: Int64?              // 1-based index of the active file
    let stalled: Bool?                   // console has stopped accepting data
    let stalledFor: Double?              // seconds the byte counter has been still

    enum CodingKeys: String, CodingKey {
        case fullPath
        case name
        case elapsedTime
        case speed
        case totalFiles
        case totalDirectories
        case filesSent
        case filesSentProgress
        case activeFileSize
        case bulkFileSize
        case status
        case note
        case indefinite
        case currentFile
        case stalled
        case stalledFor
    }
}

/// Transfer size information
struct TransferSizeInfo: Decodable {
    let total: Int64?
    let sent: Int64?
    let progress: Double?                // percent, 0-100
}

/// The phase a transfer is in, as reported by the Go layer.
///
/// `installing` is the one that matters on a Switch: DBI keeps working after
/// the last byte arrives and MTP has no completion event for it, so the UI has
/// to stop pretending a byte counter is meaningful.
enum TransferPhase: String {
    case preprocessing
    case transferring
    case installing
    case completed
    case cancelled
    case failed

    /// True when no meaningful byte progress can be reported.
    var isIndeterminate: Bool {
        self == .preprocessing || self == .installing
    }
}

/// Transfer statistics calculation and formatting
class TransferStatistics {
    let progressData: TransferProgressData
    
    init(progressData: TransferProgressData) {
        self.progressData = progressData
    }
    
    // MARK: - Base Computed Properties

    /// The reported phase of the transfer.
    var phase: TransferPhase {
        TransferPhase(rawValue: progressData.status ?? "") ?? .transferring
    }

    /// Extra detail about the current phase, when the backend supplied any.
    var note: String? {
        guard let note = progressData.note, !note.isEmpty else { return nil }
        return note
    }

    /// Whether the console has stopped accepting data.
    ///
    /// This is the difference between "slow" and "not coming back". Without it
    /// a wedged install shows a frozen percentage and no error, because from
    /// the host's side nothing has gone wrong — the writes simply never return.
    var isStalled: Bool {
        progressData.stalled ?? false
    }

    /// How long the byte counter has stood still, in seconds.
    var stalledFor: TimeInterval {
        progressData.stalledFor ?? 0
    }

    /// Elapsed time in seconds
    var elapsedTime: TimeInterval {
        progressData.elapsedTime ?? 0
    }

    /// Transfer speed in MB/s
    var speed: Double {
        (progressData.speed ?? 0.0) / (1024 * 1024)
    }
    
    /// Remaining time in seconds
    var remainingTime: TimeInterval {
        guard phase == .transferring,
              speed > 0,
              let totalSize = progressData.bulkFileSize?.total,
              let sentSize = progressData.bulkFileSize?.sent,
              totalSize > sentSize else {
            return -1
        }
        let remainingBytes = Double(totalSize - sentSize)
        let remainingMB = remainingBytes / (1024 * 1024)
        return remainingMB / speed
    }
    
    /// Overall progress percentage (0-1)
    var progressPercentage: Double {
        guard let progress = progressData.bulkFileSize?.progress else {
            return 0
        }
        return min(max(progress / 100.0, 0), 1)
    }
    
    /// Current file progress percentage (0-1)
    var activeFileProgress: Double {
        guard let progress = progressData.activeFileSize?.progress else {
            return 0
        }
        return min(max(progress / 100.0, 0), 1)
    }
    
    // MARK: - Formatted Strings
    
    /// Formatted transfer speed string
    var speedString: String {
        if phase.isIndeterminate || speed <= 0 {
            return "— MB/s"
        }
        return String(format: "%.2f MB/s", speed)
    }
    
    /// Formatted elapsed time string
    var elapsedTimeString: String {
        formatTime(elapsedTime)
    }
    
    /// Formatted remaining time string
    var remainingTimeString: String {
        if phase == .installing {
            return String(localized: "Installing on the console…")
        }
        let time = remainingTime
        guard time >= 0 else { 
            return String(localized: "Calculating...")
        }
        return formatTime(time)
    }
    
    /// Current file total size string
    var activeFileSizeString: String {
        guard let total = progressData.activeFileSize?.total else { 
            return "—"
        }
        return formatFileSize(total)
    }
    
    /// Current file transferred size string
    var activeFileSentSizeString: String {
        guard let sent = progressData.activeFileSize?.sent else { 
            return "—"
        }
        return formatFileSize(sent)
    }
    
    /// Total file size string
    var totalSizeString: String {
        guard let total = progressData.bulkFileSize?.total else { 
            return "—"
        }
        return formatFileSize(total)
    }
    
    /// Total transferred size string
    var totalSentSizeString: String {
        guard let sent = progressData.bulkFileSize?.sent else { 
            return "—"
        }
        return formatFileSize(sent)
    }
    
    /// The name of the file being transferred, in full.
    ///
    /// This is deliberately not truncated. The view that shows it truncates in
    /// the middle, which keeps both ends readable, and these names carry the
    /// title id and version at the *end* — `[01002E7016C46800][v1900544]` — so
    /// clipping the tail throws away the only part that distinguishes a base
    /// game from its own update. The debug log reads this too, and two files
    /// that cannot be told apart in a log are worse than a long line.
    var currentFileName: String {
        if let name = progressData.name, !name.isEmpty {
            return name
        }
        return "—"
    }
    
    /// File transfer progress description (e.g., "3 of 12 files")
    ///
    /// Counts the file being sent, not the ones already finished — a single-file
    /// transfer otherwise reads "0 of 1 files" for its entire duration.
    var filesProgressString: String {
        let total = progressData.totalFiles ?? 0
        guard total > 0 else { return String(localized: "—") }
        let sent = progressData.filesSent ?? 0
        let current = min(progressData.currentFile ?? (sent + 1), total)
        let shown = phase == .completed ? total : max(current, 1)
        let format = String(localized: "%d of %d files")
        return String(format: format, shown, total)
    }
    
    /// Complete progress summary for notifications or logs
    var progressSummary: String {
        let elapsed = elapsedTimeString
        let speed = speedString
        let remaining = remainingTimeString
        let percentage = progressPercentage.formatted(.percent.precision(.fractionLength(1)))
        return "\(percentage) | \(elapsed) elapsed | \(remaining) remaining | \(speed)"
    }
    
    // MARK: - Private Helper Methods
    
    /// Format file size to human-readable format
    private func formatFileSize(_ bytes: Int64) -> String {
        let units = ["B", "KB", "MB", "GB", "TB"]
        var size = Double(bytes)
        var unitIndex = 0
        
        while size >= 1024 && unitIndex < units.count - 1 {
            size /= 1024
            unitIndex += 1
        }
        
        if unitIndex == 0 {
            return String(format: "%.0f %@", size, units[unitIndex])
        } else {
            return String(format: "%.1f %@", size, units[unitIndex])
        }
    }
    
    /// Format time interval to "Xh Ym Zs" format
    private func formatTime(_ interval: TimeInterval) -> String {
        guard interval >= 0 else { return "—" }
        
        let hours = Int(interval / 3600)
        let minutes = Int((interval.truncatingRemainder(dividingBy: 3600)) / 60)
        let seconds = Int(interval.truncatingRemainder(dividingBy: 60))
        
        if hours > 0 {
            return String(format: "%dh %dm %ds", hours, minutes, seconds)
        } else if minutes > 0 {
            return String(format: "%dm %ds", minutes, seconds)
        } else {
            return String(format: "%ds", seconds)
        }
    }
}

