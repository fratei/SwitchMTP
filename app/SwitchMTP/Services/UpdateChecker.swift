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

// UpdateChecker.swift
// SwitchMTP

import Foundation

// MARK: - Update State

enum UpdateCheckState: Equatable {
    /// Idle, no check initiated yet (resets to this on every app launch)
    case idle
    /// Currently checking
    case checking
    /// A newer version is available; carries the version string and the release page URL
    case updateAvailable(version: String, releaseUrl: URL)
    /// Already on the latest version
    case upToDate
    /// Check failed (network or server error)
    case failed
}

// MARK: - UpdateChecker

@MainActor
final class UpdateChecker: ObservableObject {

    static let shared = UpdateChecker()

    // GitHub repository coordinates
    private let owner = "fratei"
    private let repo  = "SwitchMTP"

    /// State always starts as .idle on launch.
    @Published private(set) var state: UpdateCheckState = .idle

    private init() {}

    // MARK: - Public API

    func checkForUpdates() async {
        guard state != .checking else { return }
        state = .checking

        do {
            let result         = try await fetchLatestRelease()
            let currentVersion = Self.composedAppVersion()

            if isNewerVersion(result.tagName, than: currentVersion) {
                // Priority: browser_download_url from the first asset, fallback to htmlUrl
                let downloadUrl = result.downloadUrl ?? URL(string: result.htmlUrl)!
                state = .updateAvailable(version: result.tagName, releaseUrl: downloadUrl)
            } else {
                state = .upToDate
            }
        } catch {
            state = .failed
        }
    }

    // MARK: - Version helpers

    /// Combines CFBundleShortVersionString and CFBundleVersion into a single
    /// comparable string, e.g. "1.0" + "35" → "1.0.35".
    static func composedAppVersion() -> String {
        let info    = Bundle.main.infoDictionary
        let short   = info?["CFBundleShortVersionString"] as? String ?? "0.0"
        let build   = info?["CFBundleVersion"]            as? String ?? "0"
        return "\(short)(\(build))"
    }

    /// Returns true when `newVer` is strictly greater than `currentVer`.
    /// Handles an optional leading "v" prefix and compares component-by-component.
    private func isNewerVersion(_ newVer: String, than currentVer: String) -> Bool {
        let clean: (String) -> [Int] = { ver in
            let s = ver.hasPrefix("v") ? String(ver.dropFirst()) : ver
            return s.split(separator: ".").compactMap { Int($0) }
        }
        let lhs = clean(newVer)
        let rhs = clean(currentVer)
        let len = max(lhs.count, rhs.count)
        for i in 0..<len {
            let l = i < lhs.count ? lhs[i] : 0
            let r = i < rhs.count ? rhs[i] : 0
            if l != r { return l > r }
        }
        return false
    }

    // MARK: - Network

    private struct GitHubRelease: Decodable {
        struct Asset: Decodable {
            let browser_download_url: String
        }

        let tag_name: String
        let html_url: String
        let prerelease: Bool
        let assets: [Asset]

        var tagName: String { tag_name }
        var htmlUrl: String { html_url }
        
        /// Returns the browser_download_url of the first asset, if available.
        var downloadUrl: URL? {
            guard let firstAsset = assets.first else { return nil }
            return URL(string: firstAsset.browser_download_url)
        }
    }

    private func fetchLatestRelease() async throws -> GitHubRelease {
        let urlString = "https://api.github.com/repos/\(owner)/\(repo)/releases"
        guard let url = URL(string: urlString) else { throw URLError(.badURL) }

        var request = URLRequest(url: url)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.timeoutInterval = 15

        let (data, response) = try await URLSession.shared.data(for: request)
        if let httpResponse = response as? HTTPURLResponse,
           !(200..<300).contains(httpResponse.statusCode) {
            throw URLError(.badServerResponse)
        }

        let releases = try JSONDecoder().decode([GitHubRelease].self, from: data)
        guard let latest = releases.first(where: { !$0.prerelease }) else {
            throw URLError(.zeroByteResource)
        }
        return latest
    }
}
