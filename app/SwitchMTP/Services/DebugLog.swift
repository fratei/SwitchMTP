// SwitchMTP - a macOS MTP client for Nintendo Switch homebrew (DBI).
// Copyright (C) 2026 fratei
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

import Foundation

/// Appends a line to a log file inside the app container.
///
/// The app is sandboxed and normally launched by Finder, so `print` goes to a
/// block-buffered stdout that nobody reads, and `NSLog` output does not reliably
/// survive the unified log's default filtering. A plain file is the only channel
/// that works the same whether the app was started from Finder, `open`, or a
/// shell -- which matters because transfer bugs only reproduce through the GUI.
///
/// Enabled by setting the `SWITCHMTP_DEBUG_LOG` environment variable or the
/// `debugLogEnabled` user default; otherwise this is a no-op.
enum DebugLog {
    private static let queue = DispatchQueue(label: "me.fratei.switchmtp.debuglog")

    static let isEnabled: Bool = {
        if ProcessInfo.processInfo.environment["SWITCHMTP_DEBUG_LOG"] != nil { return true }
        return UserDefaults.standard.bool(forKey: "debugLogEnabled")
    }()

    /// `<container>/tmp/switchmtp-debug.log`, or nil when logging is off.
    static var url: URL? {
        guard isEnabled else { return nil }
        return FileManager.default.temporaryDirectory.appendingPathComponent("switchmtp-debug.log")
    }

    static func write(_ message: @autoclosure () -> String) {
        guard isEnabled, let url else { return }
        let line = "\(ISO8601DateFormatter().string(from: Date())) \(message())\n"
        queue.async {
            guard let data = line.data(using: .utf8) else { return }
            if let handle = try? FileHandle(forWritingTo: url) {
                defer { try? handle.close() }
                _ = try? handle.seekToEnd()
                try? handle.write(contentsOf: data)
            } else {
                try? data.write(to: url)
            }
        }
    }
}
