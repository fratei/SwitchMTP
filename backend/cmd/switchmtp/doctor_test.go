// SwitchMTP — a macOS MTP client for Nintendo Switch running DBI.
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

package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestUSBClassNameCoversTheClassesThatMatter(t *testing.T) {
	// 0x00 is the commonest value on a real bus and means "consult the
	// interface descriptors", so a bare hex code tells the reader nothing.
	if got := usbClassName(0x00); got != "per-interface" {
		t.Errorf("usbClassName(0x00) = %q", got)
	}
	// 0x06 is the class DBI's responder presents, and the one ptpcamerad on
	// macOS and gvfs on Linux both react to.
	if got := usbClassName(0x06); !strings.Contains(got, "PTP/MTP") {
		t.Errorf("usbClassName(0x06) = %q, should name PTP/MTP", got)
	}
	if got := usbClassName(0x09); got != "hub" {
		t.Errorf("usbClassName(0x09) = %q", got)
	}
	// An unknown class must still render something greppable rather than an
	// empty cell in a bug report.
	if got := usbClassName(0x42); !strings.Contains(got, "0x42") {
		t.Errorf("usbClassName(0x42) = %q, should include the raw code", got)
	}
}

// The udev rule is a Linux concept. Reporting on it anywhere else would be
// noise at best and misleading advice at worst.
func TestCheckUdevRuleOnlyAppliesToLinux(t *testing.T) {
	got := checkUdevRule()
	if runtime.GOOS == "linux" {
		if got == nil {
			t.Fatal("checkUdevRule returned nil on Linux")
		}
		if got.Detail == "" {
			t.Error("udev status must always carry an explanation")
		}
		if !got.Installed && !strings.Contains(got.Detail, "not installed") {
			t.Errorf("a missing rule should say so, got %q", got.Detail)
		}
	} else if got != nil {
		t.Errorf("checkUdevRule should return nil off Linux, got %+v", got)
	}
}

// The instructions are what the user pastes into a root shell, so the exact
// content matters more than most strings in this binary.
func TestUdevInstructionsMatchTheShippedRule(t *testing.T) {
	for _, want := range []string{
		`ATTR{idVendor}=="057e"`, // Nintendo, and only Nintendo
		`TAG+="uaccess"`,         // the modern logind grant
		`GROUP="plugdev"`,        // the Debian/Ubuntu fallback
		`ENV{ID_MTP_DEVICE}=""`,  // stops gvfs claiming the device
		"udevadm control --reload-rules",
		"69-switchmtp.rules",
	} {
		if !strings.Contains(udevInstallInstructions, want) {
			t.Errorf("udev instructions are missing %q", want)
		}
	}
	// The path it points at must be one the checker actually looks in,
	// otherwise following the advice would not clear the warning.
	if !strings.Contains(udevInstallInstructions, udevRulePaths[0]) {
		t.Errorf("instructions install to a path checkUdevRule does not check: %v", udevRulePaths)
	}
}

// The rule exists twice: as a file packagers install, and inline in the advice
// for users running a downloaded binary with no checkout. Two copies drift, so
// this pins them together — every rule line in the shipped file must appear in
// the instructions verbatim.
func TestUdevInstructionsDoNotDriftFromThePackagedFile(t *testing.T) {
	const shipped = "../../../packaging/linux/69-switchmtp.rules"

	b, err := os.ReadFile(shipped)
	if err != nil {
		t.Fatalf("cannot read the shipped udev rule at %s: %v", shipped, err)
	}

	var rules int
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rules++
		if !strings.Contains(udevInstallInstructions, line) {
			t.Errorf("the packaged rule has a line the doctor advice does not:\n  %s", line)
		}
	}
	if rules == 0 {
		t.Fatal("found no rule lines in the packaged file; the parser or the file is wrong")
	}
	t.Logf("checked %d rule lines against the doctor advice", rules)
}
