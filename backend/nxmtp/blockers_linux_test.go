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

//go:build linux

package nxmtp

import (
	"strings"
	"testing"
)

// The scanner itself is tested against a fixture in procscan_test.go, which
// runs everywhere. What can only be checked here is the real blocker table,
// because it is Linux-tagged.

// TestKnownBlockerKeysAreLowercase guards a silent failure.
//
// Lookup is knownBlockers[strings.ToLower(name)], so a key containing any
// uppercase character can never be matched. Nothing would report an error: the
// process would simply be listed as an ordinary USB holder with no advice, and
// the user would be told something has the device without being told what to do
// about it — which is the entire value of the table.
func TestKnownBlockerKeysAreLowercase(t *testing.T) {
	for name := range knownBlockers {
		if name != strings.ToLower(name) {
			t.Errorf("blocker key %q contains uppercase characters, so it can "+
				"never match: lookup lowercases the process name first", name)
		}
		if strings.TrimSpace(name) != name {
			t.Errorf("blocker key %q has surrounding whitespace, so it can never "+
				"match a trimmed process name", name)
		}
	}
}

// TestKnownBlockersAllCarryAdvice checks the table earns its place. An entry
// with no advice is worse than no entry: it marks the process as a blocker,
// which suppresses nothing and explains nothing.
func TestKnownBlockersAllCarryAdvice(t *testing.T) {
	for name, advice := range knownBlockers {
		if strings.TrimSpace(advice) == "" {
			t.Errorf("blocker %q has no advice; being told a process is to blame "+
				"without being told what to do is not a diagnosis", name)
		}
	}
}

// TestKnownBlockersCoverTheMajorDesktops is a coverage floor, not an exhaustive
// list. GNOME and KDE are the two desktops that ship an MTP handler enabled by
// default, and they are the reason this table exists at all; losing either of
// them would make the Linux port fail exactly where it is most likely to be
// tried first.
func TestKnownBlockersCoverTheMajorDesktops(t *testing.T) {
	// Process names as they appear in /proc/<pid>/comm, which is capped at 15
	// characters — that truncation is why several near-duplicate keys exist.
	required := []string{
		"gvfsd-mtp",       // GNOME, the MTP backend itself
		"gvfsd-gphoto2",   // GNOME, the PTP/gphoto2 backend
		"gvfs-mtp-volume", // GNOME, comm-truncated volume monitor
		"kiod5",           // KDE Plasma 6, kio worker process
		"mtp-probe",       // udev, transient but claims the device
	}
	for _, name := range required {
		if _, ok := knownBlockers[name]; !ok {
			t.Errorf("known blockers no longer include %q; this is one of the "+
				"handlers that claims the Switch by default", name)
		}
	}
}

// TestCommTruncatedKeysAreWithinKernelLimit pins the reason the table contains
// entries that look like typos. /proc/<pid>/comm is TASK_COMM_LEN (16) bytes
// including the terminator, so a longer process name arrives truncated to 15
// characters. Keys longer than that can still be correct — some come from
// argv-based names — but a truncated variant must exist alongside them, or the
// process will be missed on the very systems it runs on.
func TestCommTruncatedKeysAreWithinKernelLimit(t *testing.T) {
	const maxComm = 15

	for name := range knownBlockers {
		if len(name) <= maxComm {
			continue
		}
		truncated := name[:maxComm]
		if _, ok := knownBlockers[truncated]; !ok {
			t.Errorf("blocker key %q is longer than the %d characters /proc/<pid>/comm "+
				"reports, and there is no %q entry to catch the truncated form",
				name, maxComm, truncated)
		}
	}
}
