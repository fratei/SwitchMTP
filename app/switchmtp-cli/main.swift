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
import Dispatch
import Darwin

private enum ExitCode: Int32 {
    case success = 0
    case failure = 1
    case usage = 2
    case deviceNotFound = 3
}

private struct CLIError: Error {
    let message: String
    let code: ExitCode
}

private struct Config {
    var verbose = false
    var json = false
    var deviceId: String?
}

private struct RemotePath {
    let storageId: UInt32
    let path: String
}

private final class CallbackState {
    static let shared = CallbackState()
    private let lock = NSLock()
    var doneJSON: String?
    var printedProgress = false

    func setDone(_ json: String?) {
        lock.lock()
        doneJSON = json
        lock.unlock()
    }

    func reset() {
        lock.lock()
        doneJSON = nil
        printedProgress = false
        lock.unlock()
    }

    func markProgressPrinted() {
        lock.lock()
        printedProgress = true
        lock.unlock()
    }

    func consumeProgressPrinted() -> Bool {
        lock.lock()
        let value = printedProgress
        printedProgress = false
        lock.unlock()
        return value
    }
}

private let cbDone: @convention(c) (UnsafeMutablePointer<CChar>?) -> Void = { ptr in
    if CallbackState.shared.consumeProgressPrinted() {
        print("\r\u{001B}[K", terminator: "")
        fflush(stdout)
    }
    CallbackState.shared.setDone(ptr.map { String(cString: $0) })
}

private let cbPreprocess: @convention(c) (UnsafeMutablePointer<CChar>?) -> Void = { ptr in
    guard !Runtime.shared.config.json, let ptr else { return }
    let env = Envelope(json: String(cString: ptr))
    guard env.isSuccess, let data = env.data as? [String: Any] else { return }
    let files = int64(data["totalFiles"]) ?? 0
    let dirs = int64(data["totalDirectories"]) ?? 0
    let total = int64(data["totalSize"]) ?? 0
    let unknown = bool(data["sizeUnknown"]) ?? false
    let size = unknown ? "unknown size" : formatBytes(UInt64(max(total, 0)))
    print("Preparing transfer: \(files) file(s), \(dirs) director(y/ies), \(size)")
}

private let cbProgress: @convention(c) (UnsafeMutablePointer<CChar>?) -> Void = { ptr in
    guard !Runtime.shared.config.json, let ptr else { return }
    let env = Envelope(json: String(cString: ptr))
    guard env.isSuccess, let data = env.data as? [String: Any] else { return }

    let status = string(data["status"]) ?? "transferring"
    let name = string(data["name"]) ?? ""
    let speed = double(data["speed"]) ?? 0
    let speedText = "\(formatBytes(UInt64(max(speed, 0))))/s"
    let bulk = data["bulkFileSize"] as? [String: Any]
    let percent = double(bulk?["progress"] ?? 0) ?? 0
    let indefinite = bool(data["indefinite"]) ?? false
    let filesSent = int64(data["filesSent"]) ?? 0
    let totalFiles = int64(data["totalFiles"]) ?? 0

    let progressText: String
    if indefinite {
        let sent = int64(bulk?["sent"] ?? 0) ?? 0
        progressText = formatBytes(UInt64(max(sent, 0)))
    } else {
        progressText = String(format: "%5.1f%%", percent)
    }

    let line = "\r\(status) \(progressText) \(speedText) [\(filesSent)/\(totalFiles)] \(name)"
    print(line, terminator: "")
    fflush(stdout)
    CallbackState.shared.markProgressPrinted()
}

private struct Envelope {
    let raw: String
    let object: [String: Any]

    init(json: String) {
        raw = json
        if let data = json.data(using: .utf8),
           let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
            object = dict
        } else {
            object = ["errorType": "internal", "error": "Invalid JSON returned by backend", "data": NSNull()]
        }
    }

    var errorType: String { string(object["errorType"]) ?? "internal" }
    var error: String { string(object["error"]) ?? "" }
    var hint: String? { string(object["hint"]) }
    var data: Any? { object["data"] }
    var isSuccess: Bool { errorType.isEmpty }
}

private final class Runtime {
    static let shared = Runtime()
    var config = Config()
}

private enum NxmtpClient {
    static func fetchDevices() -> Envelope {
        CallbackState.shared.reset()
        NxmtpFetchAvailableDevices(cbDone)
        return result()
    }

    static func fetchDiagnostics() -> Envelope {
        CallbackState.shared.reset()
        NxmtpFetchDiagnostics(cbDone)
        return result()
    }

    static func setVerbose(_ enabled: Bool) {
        NxmtpSetVerboseLogging(enabled ? 1 : 0)
    }

    static func initialize(deviceId: String) -> Envelope {
        callWithDevice(NxmtpInitialize, deviceId: deviceId)
    }

    static func fetchDeviceInfo(deviceId: String) -> Envelope {
        callWithDevice(NxmtpFetchDeviceInfo, deviceId: deviceId)
    }

    static func fetchStorages(deviceId: String) -> Envelope {
        callWithDevice(NxmtpFetchStorages, deviceId: deviceId)
    }

    static func dispose(deviceId: String) -> Envelope {
        callWithDevice(NxmtpDispose, deviceId: deviceId)
    }

    static func walk(deviceId: String, storageId: UInt32, path: String, recursive: Bool = false, skipHidden: Bool = true) -> Envelope {
        call(NxmtpWalk, input: [
            "deviceId": deviceId,
            "storageId": storageId,
            "fullPath": normalizedRemotePath(path),
            "recursive": recursive,
            "skipDisallowedFiles": false,
            "skipHiddenFiles": skipHidden
        ])
    }

    static func makeDirectory(deviceId: String, storageId: UInt32, path: String) -> Envelope {
        call(NxmtpMakeDirectory, input: ["deviceId": deviceId, "storageId": storageId, "fullPath": normalizedRemotePath(path)])
    }

    static func delete(deviceId: String, storageId: UInt32, path: String) -> Envelope {
        call(NxmtpDeleteFile, input: ["deviceId": deviceId, "storageId": storageId, "files": [normalizedRemotePath(path)]])
    }

    static func rename(deviceId: String, storageId: UInt32, path: String, newFileName: String) -> Envelope {
        call(NxmtpRenameFile, input: [
            "deviceId": deviceId,
            "storageId": storageId,
            "fullPath": normalizedRemotePath(path),
            "newFileName": newFileName
        ])
    }

    static func download(deviceId: String, storageId: UInt32, sources: [String], destination: String) -> Envelope {
        transfer(deviceId: deviceId) {
            let input: [String: Any] = [
                "deviceId": deviceId,
                "storageId": storageId,
                "sources": sources.map(normalizedRemotePath),
                "destination": NSString(string: destination).expandingTildeInPath,
                "preprocessFiles": true
            ]
            return callTransfer(NxmtpDownloadFiles, input: input)
        }
    }

    static func upload(deviceId: String, storageId: UInt32, sources: [String], destination: String) -> Envelope {
        transfer(deviceId: deviceId) {
            let input: [String: Any] = [
                "deviceId": deviceId,
                "storageId": storageId,
                "sources": sources.map { NSString(string: $0).expandingTildeInPath },
                "destination": normalizedRemotePath(destination),
                "preprocessFiles": true
            ]
            return callTransfer(NxmtpUploadFiles, input: input)
        }
    }

    static func cancel(deviceId: String) {
        _ = callWithDevice(NxmtpCancelTransfer, deviceId: deviceId)
    }

    private static func callWithDevice(_ fn: (UnsafePointer<CChar>?, NxmtpOnCbResult?) -> Void, deviceId: String) -> Envelope {
        call(fn, input: ["deviceId": deviceId])
    }

    private static func call(_ fn: (UnsafePointer<CChar>?, NxmtpOnCbResult?) -> Void, input: [String: Any]) -> Envelope {
        CallbackState.shared.reset()
        guard let json = jsonString(input) else {
            return Envelope(json: #"{"errorType":"invalidInput","error":"Failed to encode input JSON","data":null}"#)
        }
        json.withCString { fn($0, cbDone) }
        return result()
    }

    private static func callTransfer(_ fn: (UnsafePointer<CChar>?, NxmtpOnCbResult?, NxmtpOnCbResult?, NxmtpOnCbResult?) -> Void, input: [String: Any]) -> Envelope {
        CallbackState.shared.reset()
        guard let json = jsonString(input) else {
            return Envelope(json: #"{"errorType":"invalidInput","error":"Failed to encode input JSON","data":null}"#)
        }
        json.withCString { fn($0, cbPreprocess, cbProgress, cbDone) }
        return result()
    }

    private static func transfer(deviceId: String, _ body: @escaping () -> Envelope) -> Envelope {
        signal(SIGINT, SIG_IGN)
        let source = DispatchSource.makeSignalSource(signal: SIGINT, queue: .global())
        source.setEventHandler {
            if !Runtime.shared.config.json {
                print("\nCancelling transfer…")
            }
            cancel(deviceId: deviceId)
        }
        source.resume()
        defer {
            source.cancel()
            signal(SIGINT, SIG_DFL)
        }

        let sem = DispatchSemaphore(value: 0)
        var env: Envelope?
        DispatchQueue.global(qos: .userInitiated).async {
            env = body()
            sem.signal()
        }
        sem.wait()
        return env ?? Envelope(json: #"{"errorType":"internal","error":"Transfer did not return a result","data":null}"#)
    }

    private static func result() -> Envelope {
        Envelope(json: CallbackState.shared.doneJSON ?? #"{"errorType":"internal","error":"Backend callback did not return a result","data":null}"#)
    }
}

private func printUsage() {
    print("""
    switchmtp-cli - command-line access to SwitchMTP's DBI MTP backend

    Usage:
      switchmtp-cli [--verbose] [--json] [--device <id>] <command> [arguments]

    Commands:
      devices                         List connected MTP devices
      info                            Show selected device profile and capabilities
      storages                        List storages, kinds and capabilities
      ls [-a] <storage>:<path>        List a directory
      cp <src> <dst>                  Copy local<->device; device paths are <storage>:<path>
      rm <storage>:<path>             Delete a device file or folder
      mv <storage>:<path> <new-path>  Rename within the same device directory
      mkdir <storage>:<path>          Create a device directory
      install <file...> [--nand]      Upload NSP/NSZ/XCI/XCZ to DBI install storage
      backup-saves <dir>              Download the Saves storage to a dated folder
      doctor                          Diagnose USB/MTP connection problems

    Global flags:
      --device <id>    Select a device when more than one is connected
      --json           Print raw backend JSON envelopes for the main operation
      --verbose        Enable backend protocol logging on stderr
      -h, --help       Show this help
    """)
}

private func parseArguments(_ arguments: [String]) throws -> (Config, String, [String]) {
    var config = Config()
    var rest: [String] = []
    var i = 1
    while i < arguments.count {
        let arg = arguments[i]
        switch arg {
        case "--verbose":
            config.verbose = true
        case "--json":
            config.json = true
        case "--device":
            i += 1
            guard i < arguments.count else { throw CLIError(message: "--device requires an id", code: .usage) }
            config.deviceId = arguments[i]
        default:
            rest.append(arg)
        }
        i += 1
    }
    guard let command = rest.first else { return (config, "help", []) }
    return (config, command, Array(rest.dropFirst()))
}

private func run() -> ExitCode {
    do {
        let (config, command, args) = try parseArguments(CommandLine.arguments)
        Runtime.shared.config = config
        if config.verbose { NxmtpClient.setVerbose(true) }

        switch command {
        case "-h", "--help", "help":
            printUsage()
            return .success
        case "devices":
            return handleDevices(config: config)
        case "doctor":
            return handleDoctor(config: config)
        case "info":
            let deviceId = try selectedDevice(config: config)
            return withOpenDevice(deviceId: deviceId, config: config) { details in
                if config.json { print(details.raw); return .success }
                printDeviceInfo(details)
                return .success
            }
        case "storages":
            let deviceId = try selectedDevice(config: config)
            return withOpenDevice(deviceId: deviceId, config: config) { _ in
                let env = NxmtpClient.fetchStorages(deviceId: deviceId)
                if config.json { print(env.raw) }
                if !env.isSuccess { printError(env); return exitCode(for: env) }
                if !config.json { printStorages(env) }
                return .success
            }
        case "ls":
            return try handleLs(args: args, config: config)
        case "cp":
            return try handleCp(args: args, config: config)
        case "rm":
            return try handleRm(args: args, config: config)
        case "mv":
            return try handleMv(args: args, config: config)
        case "mkdir":
            return try handleMkdir(args: args, config: config)
        case "install":
            return try handleInstall(args: args, config: config)
        case "backup-saves":
            return try handleBackupSaves(args: args, config: config)
        default:
            throw CLIError(message: "Unknown command: \(command)", code: .usage)
        }
    } catch let error as CLIError {
        if error.code == .usage { printUsage() }
        if !error.message.isEmpty { fputs("Error: \(error.message)\n", stderr) }
        return error.code
    } catch {
        fputs("Error: \(error.localizedDescription)\n", stderr)
        return .failure
    }
}

private func handleDevices(config: Config) -> ExitCode {
    let env = NxmtpClient.fetchDevices()
    if config.json { print(env.raw) }
    if !env.isSuccess { printError(env); return exitCode(for: env) }
    guard !config.json else { return .success }
    let devices = devices(from: env)
    if devices.isEmpty {
        print("[]")
        return .success
    }
    print("Device ID                         Profile     Usable  Name")
    for device in devices {
        let id = deviceId(device)
        let profile = string(device["profile"]) ?? "unknown"
        let usable = (bool(device["usable"]) ?? true) ? "yes" : "no"
        let name = string(device["displayName"]) ?? [string(device["manufacturer"]), string(device["model"])].compactMap { $0 }.joined(separator: " ")
        print("\(id.padding(toLength: 33, withPad: " ", startingAt: 0)) \(profile.padding(toLength: 11, withPad: " ", startingAt: 0)) \(usable.padding(toLength: 6, withPad: " ", startingAt: 0)) \(name)")
        if let advice = string(device["advice"]), !advice.isEmpty { print("  Hint: \(advice)") }
    }
    return .success
}

private func handleDoctor(config: Config) -> ExitCode {
    let env = NxmtpClient.fetchDiagnostics()
    if config.json { print(env.raw) }
    if !env.isSuccess { printError(env); return exitCode(for: env) }
    guard !config.json else { return .success }
    guard let data = env.data as? [String: Any] else { return .failure }
    print("SwitchMTP Doctor")
    print("================")
    print("Verdict: \(string(data["summary"]) ?? "No diagnostic summary returned.")")
    print("System: \(string(data["platform"]) ?? "unknown")/\(string(data["arch"]) ?? "unknown")")
    print("Nintendo USB device seen: \((bool(data["nintendoSeen"]) ?? false) ? "yes" : "no")")

    let mtpDevices = data["mtpDevices"] as? [[String: Any]] ?? []
    print("MTP devices: \(mtpDevices.count)")
    for device in mtpDevices {
        print("  - \(deviceId(device)) \(string(device["displayName"]) ?? string(device["model"]) ?? "MTP device")")
        if let advice = string(device["advice"]), !advice.isEmpty { print("    Hint: \(advice)") }
    }

    let blockers = data["blockers"] as? [[String: Any]] ?? []
    if !blockers.isEmpty {
        print("Processes holding USB devices:")
        for blocker in blockers {
            let pid = int64(blocker["pid"]) ?? 0
            let name = string(blocker["name"]) ?? "process"
            print("  - \(name) (pid \(pid))")
            if let advice = string(blocker["advice"]), !advice.isEmpty { print("    Hint: \(advice)") }
        }
    }

    let advice = data["advice"] as? [String] ?? []
    print("Remediation steps:")
    if advice.isEmpty {
        print("  1. No specific issue detected. If transfer still fails, reconnect the Switch and run DBI's MTP responder again.")
    } else {
        for (index, item) in advice.enumerated() { print("  \(index + 1). \(item)") }
    }
    return .success
}

private func handleLs(args: [String], config: Config) throws -> ExitCode {
    var showAll = false
    var rest = args
    if rest.first == "-a" || rest.first == "--all" {
        showAll = true
        rest.removeFirst()
    }
    guard rest.count == 1, let remote = parseRemote(rest[0]) else {
        throw CLIError(message: "ls expects <storage>:<path>", code: .usage)
    }
    let deviceId = try selectedDevice(config: config)
    return withOpenDevice(deviceId: deviceId, config: config) { _ in
        let env = NxmtpClient.walk(deviceId: deviceId, storageId: remote.storageId, path: remote.path, skipHidden: !showAll)
        if config.json { print(env.raw) }
        if !env.isSuccess { printError(env); return exitCode(for: env) }
        if !config.json { printListing(env, showAll: showAll) }
        return .success
    }
}

private func handleCp(args: [String], config: Config) throws -> ExitCode {
    guard args.count == 2 else { throw CLIError(message: "cp expects <src> <dst>", code: .usage) }
    let srcRemote = parseRemote(args[0])
    let dstRemote = parseRemote(args[1])
    guard (srcRemote == nil) != (dstRemote == nil) else {
        throw CLIError(message: "cp requires exactly one device path of the form <storage>:<path>", code: .usage)
    }
    let deviceId = try selectedDevice(config: config)
    return withOpenDevice(deviceId: deviceId, config: config) { _ in
        let env: Envelope
        if let remote = srcRemote {
            env = NxmtpClient.download(deviceId: deviceId, storageId: remote.storageId, sources: [remote.path], destination: args[1])
        } else if let remote = dstRemote {
            env = NxmtpClient.upload(deviceId: deviceId, storageId: remote.storageId, sources: [args[0]], destination: remote.path)
        } else {
            return .usage
        }
        if config.json { print(env.raw) }
        if !env.isSuccess { printError(env); return exitCode(for: env) }
        if !config.json { printTransferSummary(env, verb: "Copy") }
        return .success
    }
}

private func handleRm(args: [String], config: Config) throws -> ExitCode {
    guard args.count == 1, let remote = parseRemote(args[0]) else { throw CLIError(message: "rm expects <storage>:<path>", code: .usage) }
    guard normalizedRemotePath(remote.path) != "/" else { throw CLIError(message: "refusing to delete a storage root", code: .usage) }
    let deviceId = try selectedDevice(config: config)
    return withOpenDevice(deviceId: deviceId, config: config) { _ in
        let env = NxmtpClient.delete(deviceId: deviceId, storageId: remote.storageId, path: remote.path)
        if config.json { print(env.raw) }
        if !env.isSuccess { printError(env); return exitCode(for: env) }
        if !config.json { print("Deleted \(remote.storageId):\(normalizedRemotePath(remote.path))") }
        return .success
    }
}

private func handleMv(args: [String], config: Config) throws -> ExitCode {
    guard args.count == 2, let source = parseRemote(args[0]) else { throw CLIError(message: "mv expects <storage>:<path> <new-path>", code: .usage) }
    let newPath = parseRemote(args[1])?.path ?? args[1]
    if let dest = parseRemote(args[1]), dest.storageId != source.storageId {
        throw CLIError(message: "mv cannot move between storages", code: .usage)
    }
    let srcParent = parentPath(source.path)
    let dstParent = parentPath(newPath)
    guard srcParent == dstParent else { throw CLIError(message: "mv currently supports rename within the same directory only", code: .usage) }
    let newName = URL(fileURLWithPath: newPath).lastPathComponent
    guard !newName.isEmpty else { throw CLIError(message: "new name cannot be empty", code: .usage) }
    let deviceId = try selectedDevice(config: config)
    return withOpenDevice(deviceId: deviceId, config: config) { _ in
        let env = NxmtpClient.rename(deviceId: deviceId, storageId: source.storageId, path: source.path, newFileName: newName)
        if config.json { print(env.raw) }
        if !env.isSuccess { printError(env); return exitCode(for: env) }
        if !config.json { print("Renamed to \(newName)") }
        return .success
    }
}

private func handleMkdir(args: [String], config: Config) throws -> ExitCode {
    guard args.count == 1, let remote = parseRemote(args[0]) else { throw CLIError(message: "mkdir expects <storage>:<path>", code: .usage) }
    let deviceId = try selectedDevice(config: config)
    return withOpenDevice(deviceId: deviceId, config: config) { _ in
        let env = NxmtpClient.makeDirectory(deviceId: deviceId, storageId: remote.storageId, path: remote.path)
        if config.json { print(env.raw) }
        if !env.isSuccess { printError(env); return exitCode(for: env) }
        if !config.json { print("Created \(remote.storageId):\(normalizedRemotePath(remote.path))") }
        return .success
    }
}

private func handleInstall(args: [String], config: Config) throws -> ExitCode {
    var toNand = false
    var files: [String] = []
    for arg in args {
        if arg == "--nand" { toNand = true } else { files.append(arg) }
    }
    guard !files.isEmpty else { throw CLIError(message: "install expects at least one file", code: .usage) }
    let allowed = ["nsp", "nsz", "xci", "xcz"]
    for file in files {
        let ext = URL(fileURLWithPath: file).pathExtension.lowercased()
        guard allowed.contains(ext) else { throw CLIError(message: "install only accepts .nsp, .nsz, .xci and .xcz files", code: .usage) }
    }
    let deviceId = try selectedDevice(config: config)
    return withOpenDevice(deviceId: deviceId, config: config) { _ in
        let storageEnv = NxmtpClient.fetchStorages(deviceId: deviceId)
        if !storageEnv.isSuccess { if config.json { print(storageEnv.raw) } else { printError(storageEnv) }; return exitCode(for: storageEnv) }
        guard let storage = storages(from: storageEnv).first(where: { (string($0["kind"]) ?? "") == (toNand ? "nandInstall" : "sdInstall") }),
              let sid = uint32(storage["Sid"]) else {
            fputs("Error: DBI \(toNand ? "NAND" : "SD") install storage was not found. Start DBI's MTP responder on the Switch.\n", stderr)
            return .deviceNotFound
        }
        if !config.json {
            print("Uploading to \(toNand ? "NAND" : "SD") install target. Watch the Switch screen after transfer completes.")
        }
        let env = NxmtpClient.upload(deviceId: deviceId, storageId: sid, sources: files, destination: "/")
        if config.json { print(env.raw) }
        if !env.isSuccess { printError(env); return exitCode(for: env) }
        if !config.json {
            printTransferSummary(env, verb: "Install upload")
            print("Installation continues on the Switch. MTP does not report final installation completion.")
        }
        return .success
    }
}

private func handleBackupSaves(args: [String], config: Config) throws -> ExitCode {
    guard args.count == 1 else { throw CLIError(message: "backup-saves expects a destination directory", code: .usage) }
    let root = NSString(string: args[0]).expandingTildeInPath
    let formatter = DateFormatter()
    formatter.dateFormat = "yyyy-MM-dd_HHmmss"
    let destination = (root as NSString).appendingPathComponent("switch-saves-\(formatter.string(from: Date()))")

    let deviceId = try selectedDevice(config: config)
    return withOpenDevice(deviceId: deviceId, config: config) { _ in
        let storageEnv = NxmtpClient.fetchStorages(deviceId: deviceId)
        if !storageEnv.isSuccess { if config.json { print(storageEnv.raw) } else { printError(storageEnv) }; return exitCode(for: storageEnv) }
        guard let storage = storages(from: storageEnv).first(where: { (string($0["kind"]) ?? "") == "saves" }),
              let sid = uint32(storage["Sid"]) else {
            fputs("Error: Saves storage was not found. Start DBI's MTP responder on the Switch.\n", stderr)
            return .deviceNotFound
        }
        if !config.json { print("Backing up saves to \(destination)") }
        let env = NxmtpClient.download(deviceId: deviceId, storageId: sid, sources: ["/"], destination: destination)
        if config.json { print(env.raw) }
        if !env.isSuccess { printError(env); return exitCode(for: env) }
        if !config.json { printTransferSummary(env, verb: "Backup"); print("Saved to \(destination)") }
        return .success
    }
}

private func withOpenDevice(deviceId: String, config: Config, body: (Envelope) -> ExitCode) -> ExitCode {
    let details = NxmtpClient.initialize(deviceId: deviceId)
    if !details.isSuccess {
        if config.json { print(details.raw) } else { printError(details) }
        return exitCode(for: details)
    }
    defer { _ = NxmtpClient.dispose(deviceId: deviceId) }
    return body(details)
}

private func selectedDevice(config: Config) throws -> String {
    if let deviceId = config.deviceId { return deviceId }
    let env = NxmtpClient.fetchDevices()
    if !env.isSuccess { printError(env); throw CLIError(message: "", code: exitCode(for: env)) }
    let usable = devices(from: env).filter { bool($0["usable"]) ?? true }
    if usable.isEmpty {
        throw CLIError(message: "No MTP device is connected. Connect the Switch, open DBI's MTP responder, then try again.", code: .deviceNotFound)
    }
    if usable.count > 1 {
        throw CLIError(message: "Multiple MTP devices are connected. Pass --device <id>.", code: .usage)
    }
    return deviceId(usable[0])
}

private func printDeviceInfo(_ env: Envelope) {
    guard let data = env.data as? [String: Any] else { return }
    print("Device Information")
    print("==================")
    print("Device ID: \(string(data["deviceId"]) ?? "-")")
    print("Name:      \(string(data["displayName"]) ?? "-")")
    print("Profile:   \(string(data["deviceProfile"]) ?? "unknown")")
    if let advice = string(data["advice"]), !advice.isEmpty { print("Hint:      \(advice)") }

    if let mtp = data["mtpDeviceInfo"] as? [String: Any] {
        print("MTP:")
        for key in ["Manufacturer", "Model", "SerialNumber", "DeviceVersion", "MTPExtension"] {
            if let value = string(mtp[key]), !value.isEmpty { print("  \(key): \(value)") }
        }
    }
    if let caps = data["capabilities"] as? [String: Any] {
        let enabled = caps.keys.sorted().filter { bool(caps[$0]) == true }
        print("Capabilities: \(enabled.isEmpty ? "none reported" : enabled.joined(separator: ", "))")
    }
}

private func printStorages(_ env: Envelope) {
    let list = storages(from: env)
    if list.isEmpty { print("No storages reported."); return }
    print("ID          Kind             Capabilities                 Name")
    for storage in list {
        let sid = String(uint32(storage["Sid"]) ?? 0)
        let kind = string(storage["kind"]) ?? "unknown"
        let caps = capabilityText(storage["capabilities"] as? [String: Any] ?? [:])
        let name = string(storage["displayName"]) ?? storageDescription(storage)
        print("\(sid.padding(toLength: 11, withPad: " ", startingAt: 0)) \(kind.padding(toLength: 16, withPad: " ", startingAt: 0)) \(caps.padding(toLength: 28, withPad: " ", startingAt: 0)) \(name)")
        var notes: [String] = []
        if bool(storage["virtual"]) == true { notes.append("virtual/generated") }
        if bool((storage["capabilities"] as? [String: Any])?["installTarget"]) == true { notes.append("write-only install trigger") }
        if bool((storage["capabilities"] as? [String: Any])?["browse"]) == false { notes.append("not browsable") }
        if let description = string(storage["description"]), !description.isEmpty { notes.append(description) }
        if !notes.isEmpty { print("  \(notes.joined(separator: "; "))") }
    }
}

private func printListing(_ env: Envelope, showAll: Bool) {
    let entries = (env.data as? [[String: Any]] ?? []).filter { showAll || !(string($0["name"]) ?? "").hasPrefix(".") }
    if entries.isEmpty { return }
    for entry in entries.sorted(by: { (string($0["name"]) ?? "") < (string($1["name"]) ?? "") }) {
        let type = (bool(entry["isFolder"]) ?? false) ? "<DIR>" : "     "
        let sizeText: String
        if bool(entry["isFolder"]) == true {
            sizeText = "-"
        } else if bool(entry["sizeUnknown"]) == true {
            sizeText = "—"
        } else {
            sizeText = formatBytes(UInt64(max(int64(entry["size"]) ?? 0, 0)))
        }
        let date = string(entry["dateAdded"]) ?? ""
        let name = string(entry["name"]) ?? ""
        print("\(type) \(sizeText.padding(toLength: 12, withPad: " ", startingAt: 0)) \(date.padding(toLength: 24, withPad: " ", startingAt: 0)) \(name)")
    }
}

private func printTransferSummary(_ env: Envelope, verb: String) {
    guard let data = env.data as? [String: Any] else { print("\(verb) complete."); return }
    let files = int64(data["totalFiles"]) ?? 0
    let dirs = int64(data["totalDirectories"]) ?? 0
    let bytes = int64(data["totalBytes"]) ?? 0
    let elapsed = double(data["elapsed"]) ?? 0
    print("\(verb) complete: \(files) file(s), \(dirs) director(y/ies), \(formatBytes(UInt64(max(bytes, 0)))) in \(String(format: "%.1f", elapsed))s")
    if let note = string(data["note"]), !note.isEmpty { print(note) }
    if let skipped = data["skipped"] as? [String], !skipped.isEmpty { print("Skipped: \(skipped.joined(separator: ", "))") }
}

private func printError(_ env: Envelope) {
    let message = env.error.isEmpty ? "Unknown backend error" : env.error
    fputs("Error [\(env.errorType)]: \(message)\n", stderr)
    if let hint = env.hint, !hint.isEmpty { fputs("Hint: \(hint)\n", stderr) }
}

private func exitCode(for env: Envelope) -> ExitCode {
    switch env.errorType {
    case "deviceNotFound", "deviceDisconnected": return .deviceNotFound
    case "invalidInput": return .usage
    default: return .failure
    }
}

private func parseRemote(_ value: String) -> RemotePath? {
    guard let colon = value.firstIndex(of: ":") else { return nil }
    let sidText = String(value[..<colon])
    guard let sid = UInt32(sidText) else { return nil }
    let pathStart = value.index(after: colon)
    let rawPath = String(value[pathStart...])
    return RemotePath(storageId: sid, path: rawPath.isEmpty ? "/" : rawPath)
}

private func normalizedRemotePath(_ path: String) -> String {
    let trimmed = path.trimmingCharacters(in: .whitespacesAndNewlines)
    if trimmed.isEmpty { return "/" }
    return trimmed.hasPrefix("/") ? trimmed : "/\(trimmed)"
}

private func parentPath(_ path: String) -> String {
    let normalized = normalizedRemotePath(path)
    guard normalized != "/" else { return "/" }
    let ns = normalized as NSString
    let parent = ns.deletingLastPathComponent
    return parent.isEmpty ? "/" : parent
}

private func devices(from env: Envelope) -> [[String: Any]] {
    env.data as? [[String: Any]] ?? []
}

private func storages(from env: Envelope) -> [[String: Any]] {
    env.data as? [[String: Any]] ?? []
}

private func deviceId(_ device: [String: Any]) -> String {
    let vendor = uint32(device["vendorId"]) ?? 0
    let product = uint32(device["productId"]) ?? 0
    let serial = string(device["serialNumber"]) ?? ""
    return "\(vendor)|\(product)|\(serial)"
}

private func storageDescription(_ storage: [String: Any]) -> String {
    if let info = storage["Info"] as? [String: Any] {
        return string(info["StorageDescription"]) ?? string(info["VolumeLabel"]) ?? "Storage"
    }
    return "Storage"
}

private func capabilityText(_ caps: [String: Any]) -> String {
    var values: [String] = []
    for key in ["browse", "read", "write", "delete", "rename", "makeDirectory", "installTarget"] {
        if bool(caps[key]) == true { values.append(key) }
    }
    return values.isEmpty ? "none" : values.joined(separator: ",")
}

private func jsonString(_ value: [String: Any]) -> String? {
    guard JSONSerialization.isValidJSONObject(value),
          let data = try? JSONSerialization.data(withJSONObject: value, options: []) else { return nil }
    return String(data: data, encoding: .utf8)
}

private func formatBytes(_ bytes: UInt64) -> String {
    let units = ["B", "KB", "MB", "GB", "TB"]
    var size = Double(bytes)
    var unit = 0
    while size >= 1024, unit < units.count - 1 {
        size /= 1024
        unit += 1
    }
    return unit == 0 ? String(format: "%.0f %@", size, units[unit]) : String(format: "%.2f %@", size, units[unit])
}

private func string(_ value: Any?) -> String? {
    switch value {
    case let value as String: return value
    case let value as NSNumber: return value.stringValue
    default: return nil
    }
}

private func bool(_ value: Any?) -> Bool? {
    switch value {
    case let value as Bool: return value
    case let value as NSNumber: return value.boolValue
    case let value as String: return ["true", "yes", "1"].contains(value.lowercased())
    default: return nil
    }
}

private func int64(_ value: Any?) -> Int64? {
    switch value {
    case let value as NSNumber: return value.int64Value
    case let value as String: return Int64(value)
    default: return nil
    }
}

private func uint32(_ value: Any?) -> UInt32? {
    switch value {
    case let value as NSNumber: return UInt32(truncating: value)
    case let value as String: return UInt32(value)
    default: return nil
    }
}

private func double(_ value: Any?) -> Double? {
    switch value {
    case let value as NSNumber: return value.doubleValue
    case let value as String: return Double(value)
    default: return nil
    }
}

exit(run().rawValue)
