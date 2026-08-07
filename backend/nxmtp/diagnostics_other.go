// SwitchMTP — an MTP client for Nintendo Switch running DBI.
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

//go:build !darwin && !linux

package nxmtp

// Fallback diagnostics for platforms without a device-ownership API we can
// query. Everything else in the report -- USB enumeration, Switch detection,
// remediation advice -- still works, because that lives in diagnostics.go.
//
// Windows is the platform this actually covers today. Its story is different
// enough to deserve its own file when it is implemented: MTP devices there are
// bound to the in-box WPD driver, so the question is not "which process holds
// the device" but "which driver is bound to it".

// platformHostNoun names the host in user-facing advice.
const platformHostNoun = "this computer"

// platformNoInterfaceAdvice and platformNoUSBAdvice add platform-specific steps
// to the shared advice in summarise.
var (
	platformNoInterfaceAdvice = []string{}
	platformNoUSBAdvice       = []string{
		"Check that SwitchMTP has permission to access USB devices, then restart it.",
	}
)

// knownBlockers is empty here: without a way to list the processes holding a
// device, there is nothing to match names against.
var knownBlockers = map[string]string{}

// usbClients cannot be implemented portably. Returning nil is honest: the
// report says "no known blockers" rather than claiming the device is free.
func usbClients(selfPID int) []USBClient { return nil }

// interfaceHolders likewise has no portable equivalent.
func interfaceHolders(d *Diagnostics) []USBClient { return nil }
