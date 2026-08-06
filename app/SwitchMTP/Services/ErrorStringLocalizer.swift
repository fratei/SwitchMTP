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
        
        if let aiError = localizeAIErrors(errorString) {
            return aiError
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
    
    private static func localizeAIErrors(_ errorString: String) -> String? {
        switch errorString {
        case "No directory context available. Please make sure the file list has loaded before using AI.":
            return String(localized: "No directory context available. Please make sure the file list has loaded before using AI.")
        case "No query specified.":
            return String(localized: "No query specified.")
        case "AI is not configured. Please set up an AI provider in Settings → AI.":
            return String(localized: "AI is not configured. Please set up an AI provider in Settings → AI.")
        case "This feature is currently only available in API mode. Apple Foundation Models are not supported yet.":
            return String(localized: "This feature is currently only available in API mode. Apple Foundation Models are not supported yet.")
        case "API configuration is incomplete. Please check Settings → AI.":
            return String(localized: "API configuration is incomplete. Please check Settings → AI.")
        case "Failed to construct request.":
            return String(localized: "Failed to construct request.")
        case "Invalid API Key. Please check your settings.":
            return String(localized: "Invalid API Key. Please check your settings.")
        case "API endpoint not found. Please check the URL.":
            return String(localized: "API endpoint not found. Please check the URL.")
        case "Rate limit exceeded. Please try again later.":
            return String(localized: "Rate limit exceeded. Please try again later.")
        case "Invalid server response.":
            return String(localized: "Invalid server response.")
        case "Request timed out. The server took too long to respond.":
            return String(localized: "Request timed out. The server took too long to respond.")
        case "No internet connection detected.":
            return String(localized: "No internet connection detected.")
        case "Could not find the AI server. Check the URL.":
            return String(localized: "Could not find the AI server. Check the URL.")
        case "SSL/TLS connection failed.":
            return String(localized: "SSL/TLS connection failed.")
        case "Apple Intelligence hasn't been turned on.":
            return String(localized: "Apple Intelligence hasn't been turned on.")
        case "Model is not ready yet. Try again later.":
            return String(localized: "Model is not ready yet. Try again later.")
        case "Your Mac is not eligible for Apple Intelligence.":
            return String(localized: "Your Mac is not eligible for Apple Intelligence.")
        case "Not available for unknown reasons.":
            return String(localized: "Not available for unknown reasons.")
        case "Apple Foundation Models require macOS 26 or later with Apple Intelligence enabled.":
            return String(localized: "Apple Foundation Models require macOS 26 or later with Apple Intelligence enabled.")
        case "Apple Foundation Models are not available. Build with Xcode 26+ targeting macOS 26+.":
            return String(localized: "Apple Foundation Models are not available. Build with Xcode 26+ targeting macOS 26+.")
        case "Apple Foundation Model returned an empty response.":
            return String(localized: "Apple Foundation Model returned an empty response.")
        default:
            break
        }
        
        let lowercased = errorString.lowercased()
        if lowercased.hasPrefix("server returned error") {
            let suffix = errorString.dropFirst("Server returned error".count)
            return String(localized: "Server returned error") + String(suffix)
        }
        if lowercased.hasPrefix("server error") {
            let suffix = errorString.dropFirst("Server error".count)
            return String(localized: "Server error") + String(suffix)
        }
        if lowercased.hasPrefix("failed to parse ai response") {
            let suffix = errorString.dropFirst("Failed to parse AI response".count)
            return String(localized: "Failed to parse AI response") + String(suffix)
        }
        if lowercased.hasPrefix("network error:") {
            let suffix = errorString.dropFirst("Network error:".count)
            return String(localized: "Network error:") + String(suffix)
        }
        if lowercased.hasPrefix("unexpected error:") {
            let suffix = errorString.dropFirst("Unexpected error:".count)
            return String(localized: "Unexpected error:") + String(suffix)
        }
        if lowercased.hasPrefix("error:") {
            let suffix = errorString.dropFirst("Error:".count)
            return String(localized: "Error:") + String(suffix)
        }
        if lowercased.hasPrefix("apple foundation model error:") {
            let suffix = errorString.dropFirst("Apple Foundation Model error:".count)
            return String(localized: "Apple Foundation Model error:") + String(suffix)
        }
        
        return nil
    }
    
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
