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
struct TransferProgressData: Decodable {
    let fullPath: String?
    let name: String?
    let elapsedTime: Int64?              // milliseconds
    let speed: Double?                   // MB/s
    let totalFiles: Int64?
    let totalDirectories: Int64?
    let filesSent: Int64?
    let filesSentProgress: Float?
    let activeFileSize: TransferSizeInfo?
    let bulkFileSize: TransferSizeInfo?
    let status: String?                  // transfer status
    
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
    }
}

/// Transfer size information
struct TransferSizeInfo: Decodable {
    let total: Int64?
    let sent: Int64?
    let progress: Float?
}

/// Transfer statistics calculation and formatting
class TransferStatistics {
    let progressData: TransferProgressData
    
    init(progressData: TransferProgressData) {
        self.progressData = progressData
    }
    
    // MARK: - Base Computed Properties
    
    /// Elapsed time in seconds
    var elapsedTime: TimeInterval {
        TimeInterval((progressData.elapsedTime ?? 0) / 1000)
    }
    
    /// Transfer speed in MB/s
    var speed: Double {
        progressData.speed ?? 0.0
    }
    
    /// Remaining time in seconds
    var remainingTime: TimeInterval {
        guard speed > 0,
              let totalSize = progressData.bulkFileSize?.total,
              let sentSize = progressData.bulkFileSize?.sent else {
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
        return Double(progress) / 100.0
    }
    
    /// Current file progress percentage (0-1)
    var activeFileProgress: Double {
        guard let progress = progressData.activeFileSize?.progress else {
            return 0
        }
        return Double(progress) / 100.0
    }
    
    // MARK: - Formatted Strings
    
    /// Formatted transfer speed string
    var speedString: String {
        if speed <= 0 {
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
    
    /// Current filename with truncation handling
    var currentFileName: String {
        if let name = progressData.name, !name.isEmpty {
            // Truncate long filenames to 50 characters
            if name.count > 50 {
                let index = name.index(name.startIndex, offsetBy: 47)
                return String(name[..<index]) + "..."
            }
            return name
        }
        return "—"
    }
    
    /// File transfer progress description (e.g., "120 of 500 files")
    var filesProgressString: String {
        let sent = progressData.filesSent ?? 0
        let total = progressData.totalFiles ?? 0
        if total > 0 {
            let format = String(localized: "%d of %d files")
            return String(format: format, sent, total)
        }
        return String(localized: "—")
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

