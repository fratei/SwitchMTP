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

struct MTPDeviceInfo: Codable, Identifiable, Hashable {
    let vendorId: UInt16
    let productId: UInt16
    let serialNumber: String
    let manufacturer: String
    let model: String
    
    /// Unique identifier used for directed connections in the Go backend.
    /// Format: "vendorId|productId|serialNumber"
    var id: String {
        return "\(vendorId)|\(productId)|\(serialNumber)"
    }
    
    /// User-friendly name to display in the UI.
    var displayName: String {
        if model.isEmpty && manufacturer.isEmpty {
            return String(localized: "Unknown Device")
        }
        if model.isEmpty {
            return manufacturer
        }
        if manufacturer.isEmpty {
            return model
        }
//        return "\(manufacturer) \(model)"
        return "\(model)"
    }
}
