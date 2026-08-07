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

/// Localizes backend error strings to user-friendly messages in the current language.
struct ErrorStringLocalizer {
    
    // MARK: - Error Pattern Recognition
    
    /// Determines if the error is a device disconnection error
    static func isDeviceDisconnectedError(_ errorString: String) -> Bool {
        return errorString.contains("ErrorDeviceChanged") ||
               errorString.contains("LIBUSB_ERROR_NO_DEVICE") ||
               errorString.contains("Device disconnected")
    }
    
    /// Determines if the error is a transfer cancellation (user-initiated)
    static func isTransferCancelledError(_ errorString: String) -> Bool {
        let lowercased = errorString.lowercased()
        return lowercased.contains("context canceled") ||
               lowercased.contains("transfer cancelled by user") ||
               lowercased.contains("cancelled")
    }
    
    /// Determines if the error is a permission/access denied error
    static func isPermissionError(_ errorString: String) -> Bool {
        return errorString.contains("permission") ||
               errorString.contains("access denied") ||
               errorString.contains("Permission denied") ||
               errorString.contains("EPERM")
    }
    
    /// Determines if the error is a not found error
    static func isNotFoundError(_ errorString: String) -> Bool {
        return errorString.contains("not found") ||
               errorString.contains("no such file") ||
               errorString.contains("ENOENT")
    }
    
    /// Determines if the error is a space/storage error
    static func isStorageError(_ errorString: String) -> Bool {
        return errorString.contains("space") ||
               errorString.contains("disk full") ||
               errorString.contains("ENOSPC")
    }
    
    // MARK: - Localization
    
    /// Localizes the error string to the user's preferred language
    /// - Parameter errorString: Raw error string from backend
    /// - Returns: User-friendly localized error message
    static func localize(_ errorString: String) -> String {
        guard !errorString.isEmpty else {
            return String(localized: "Unknown error")
        }
        
        // Check for device disconnection (highest priority)
        if isDeviceDisconnectedError(errorString) {
            return String(localized: "Device disconnected")
        }

        // Check for transfer cancellation
        if isTransferCancelledError(errorString) {
            return String(localized: "Transfer cancelled")
        }
        
        // Check for specific error patterns and return localized versions
        if isPermissionError(errorString) {
            return String(localized: "Permission denied")
        }
        
        if isNotFoundError(errorString) {
            return String(localized: "File or folder not found")
        }
        
        if isStorageError(errorString) {
            return String(localized: "Storage space full")
        }
        
        // Try to extract error type prefix (e.g., "ErrorDeviceChanged: some message")
        if let colonIndex = errorString.firstIndex(of: ":") {
            let errorType = String(errorString[..<colonIndex]).trimmingCharacters(in: .whitespaces)
            let errorMessage = String(errorString[errorString.index(after: colonIndex)...]).trimmingCharacters(in: .whitespaces)
            
            // Localize the error type
            let localizedType = localizeErrorType(errorType)
            
            // If we have a detailed message, append it
            if !errorMessage.isEmpty && errorMessage != errorType {
                return "\(localizedType): \(errorMessage)"
            }
            return localizedType
        }
        
        // Fallback: try to localize common error keywords
        let localizedError = localizeCommonErrors(errorString)
        return localizedError
    }
    
    // MARK: - Private Helpers
    
    private static func localizeErrorType(_ errorType: String) -> String {
        switch errorType {
        // --- Nxmtp backend error types (from send_to_js/enums.go) ---
        case "ErrorMtpDetectFailed":
            return String(localized: "MTP detection failed")
        case "ErrorMtpLockExists":
            return String(localized: "MTP operation busy")
        case "ErrorDeviceChanged":
            return String(localized: "Device changed")
        case "ErrorDeviceSetup":
            return String(localized: "Device setup failed")
        case "ErrorMultipleDevice":
            return String(localized: "Multiple devices connected")
        case "ErrorAllowStorageAccess":
            return String(localized: "Allow storage access on device")
        case "ErrorDeviceLocked":
            return String(localized: "Device is busy")
        case "ErrorDeviceInfo":
            return String(localized: "Cannot read device info")
        case "ErrorStorageInfo":
            return String(localized: "Cannot read storage info")
        case "ErrorNoStorage":
            return String(localized: "No storage found")
        case "ErrorStorageFull":
            return String(localized: "Storage space full")
        case "ErrorListDirectory":
            return String(localized: "Failed to list directory")
        case "ErrorFileNotFound":
            return String(localized: "File not found")
        case "ErrorFilePermission":
            return String(localized: "Permission denied")
        case "ErrorLocalFileRead":
            return String(localized: "Local file read error")
        case "ErrorInvalidPath":
            return String(localized: "Invalid path")
        case "ErrorFileTransfer":
            return String(localized: "File transfer error")
        case "ErrorFileObjectRead":
            return String(localized: "File object read error")
        case "ErrorSendObject":
            return String(localized: "Failed to send file")
        case "ErrorGeneral":
            return String(localized: "An unexpected error occurred")
        // --- Legacy/fallback error type names ---
        case "ErrorDeviceNotFound":
            return String(localized: "Device not found")
        case "ErrorStorages":
            return String(localized: "Storage error")
        case "ErrorWalk":
            return String(localized: "Failed to read directory")
        case "ErrorDelete":
            return String(localized: "Failed to delete")
        case "ErrorUpload":
            return String(localized: "Import failed")
        case "ErrorDownload":
            return String(localized: "Export failed")
        case "ErrorMakeDirectory":
            return String(localized: "Failed to create folder")
        case "ErrorFileExists":
            return String(localized: "File operation failed")
        case "ErrorRename":
            return String(localized: "Failed to rename")
        default:
            return errorType
        }
    }
    
    private static func localizeCommonErrors(_ errorString: String) -> String {
        let lowercased = errorString.lowercased()
        
        // USB-related errors
        if lowercased.contains("libusb") || lowercased.contains("usb") {
            if lowercased.contains("no device") {
                return String(localized: "Device not found")
            }
            if lowercased.contains("permission") || lowercased.contains("access") {
                return String(localized: "Cannot access device")
            }
            return String(localized: "USB communication error")
        }
        
        // File system errors
        if lowercased.contains("file") || lowercased.contains("directory") {
            if lowercased.contains("not found") {
                return String(localized: "File or folder not found")
            }
            if lowercased.contains("permission") {
                return String(localized: "Cannot access file")
            }
            if lowercased.contains("exist") {
                return String(localized: "File already exists")
            }
        }
        
        // Network errors
        if lowercased.contains("timeout") || lowercased.contains("connection") {
            return String(localized: "Connection error")
        }
        
        // Memory/system errors
        if lowercased.contains("memory") || lowercased.contains("malloc") {
            return String(localized: "System memory error")
        }
        
        // Default: return the original error string if no pattern matches
        return errorString
    }
}
