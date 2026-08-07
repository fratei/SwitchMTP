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

import AppKit
import Foundation

/// Opens a prefilled bug report on GitHub.
///
/// Reports are only as good as the environment details in them, and those are
/// exactly the details people leave out — so the app fills them in rather than
/// asking. The diagnostics report is put on the clipboard at the same time,
/// because it is the single most useful thing in a connection or transfer
/// report and it is otherwise two menus away.
enum IssueReporter {
    /// Matches `.github/ISSUE_TEMPLATE/1-bug-report.yml`. GitHub resolves this
    /// by filename, so renaming that file breaks this link.
    private static let bugTemplate = "1-bug-report.yml"
    private static let base = "https://github.com/fratei/SwitchMTP"

    /// Field `id`s from the bug form, and only `input` ones.
    ///
    /// GitHub documents prefilling `input` and `textarea` fields by `id`.
    /// Prefilling a `dropdown` — and especially a multi-select one — is not
    /// documented, and a value the form does not recognise is worse than an
    /// empty field, so hardware and install source are deliberately left for
    /// the reporter to choose.
    static func bugReportURL() -> URL? {
        var components = URLComponents(string: "\(base)/issues/new")
        components?.queryItems = [
            URLQueryItem(name: "template", value: bugTemplate),
            URLQueryItem(name: "version", value: appVersion()),
            URLQueryItem(name: "macos", value: systemVersion()),
        ]
        return components?.url
    }

    /// `1.0.1 (1)` — the same string shown in the About panel, so a reporter
    /// quoting either one gives the same answer.
    static func appVersion() -> String {
        let info = Bundle.main.infoDictionary
        let short = info?["CFBundleShortVersionString"] as? String ?? "unknown"
        let build = info?["CFBundleVersion"] as? String ?? "?"
        return "\(short) (\(build))"
    }

    static func systemVersion() -> String {
        let v = ProcessInfo.processInfo.operatingSystemVersion
        return v.patchVersion == 0
            ? "\(v.majorVersion).\(v.minorVersion)"
            : "\(v.majorVersion).\(v.minorVersion).\(v.patchVersion)"
    }

    /// Apple Silicon or Intel. Shown to the reporter so they can pick the right
    /// option; not sent in the URL, because the field is a dropdown.
    static func architecture() -> String {
        #if arch(arm64)
        return String(localized: "Apple Silicon")
        #else
        return String(localized: "Intel")
        #endif
    }

    static func open() {
        guard let url = bugReportURL() else { return }
        NSWorkspace.shared.open(url)
    }
}
