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

package nxmtp

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// This file holds the /proc scanning logic used on Linux to find processes
// holding a USB device. It carries no build tag on purpose.
//
// The logic is only *meaningful* on Linux, but it is not Linux-specific code:
// it reads directories and resolves symlinks, which every platform can do. The
// part that is genuinely Linux-only is a pair of path constants. Splitting on
// what actually differs — rather than on which OS the feature targets — is what
// lets this be tested against a synthetic /proc tree on any machine, including
// the macOS one it will usually be written on.
//
// That matters more here than it looks. A CI runner has no USB devices, so
// running the real scanner there proves only that it does not crash. Every
// interesting behaviour — matching a blocker by name, skipping another user's
// unreadable process, not reporting ourselves as a blocker — is invisible
// without a fixture.

// procScan describes where to look and what to look for. The roots are
// parameters rather than constants purely so tests can supply a fixture.
type procScan struct {
	// procRoot is /proc in production.
	procRoot string
	// usbPrefix is the path prefix an open USB file descriptor resolves to.
	// Every libusb handle is an open fd on /dev/bus/usb/<bus>/<device>, which
	// is what makes this scan possible at all.
	usbPrefix string
	// selfPID marks our own process so it is never reported as a blocker.
	selfPID int
	// blockers maps a lowercased process name to the advice for it.
	blockers map[string]string
}

// scanProc lists processes holding an open handle on a USB device.
//
// Processes owned by other users are silently skipped: their fd directories are
// unreadable without root. That is acceptable rather than merely tolerable —
// the blockers that matter, the desktop MTP backends, run as the same user we
// do, because they are started by the user's own session.
func scanProc(cfg procScan) []USBClient {
	entries, err := os.ReadDir(cfg.procRoot)
	if err != nil {
		return nil
	}

	var out []USBClient
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// /proc holds plenty of non-numeric entries; a numeric name is what
		// distinguishes a process from "self", "net", "sys" and the rest.
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if !holdsUSBDevice(cfg, pid) {
			continue
		}

		name := processName(cfg, pid)
		c := USBClient{PID: pid, Name: name, IsSelf: pid == cfg.selfPID}
		if advice, ok := cfg.blockers[strings.ToLower(name)]; ok && !c.IsSelf {
			c.Known, c.Blocker, c.Advice = true, true, advice
		}
		out = append(out, c)
	}

	// Blockers first, then by PID: the actionable entries should be at the top
	// of a diagnostics report, not buried among unrelated processes.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Blocker != out[j].Blocker {
			return out[i].Blocker
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// holdsUSBDevice reports whether a process has any USB file descriptor open.
// An unreadable fd directory means "another user's process", not "no".
func holdsUSBDevice(cfg procScan, pid int) bool {
	fdDir := filepath.Join(cfg.procRoot, strconv.Itoa(pid), "fd")
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return false
	}
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, cfg.usbPrefix) {
			return true
		}
	}
	return false
}

// processName reads a process's short name from comm, falling back to the PID
// so a report never contains a blank where a name should be.
func processName(cfg procScan, pid int) string {
	raw, err := os.ReadFile(filepath.Join(cfg.procRoot, strconv.Itoa(pid), "comm"))
	if err != nil {
		return "pid " + strconv.Itoa(pid)
	}
	name := strings.TrimSpace(string(raw))
	if name == "" {
		return "pid " + strconv.Itoa(pid)
	}
	return name
}
