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
	"strconv"
	"testing"
)

// These tests build a synthetic /proc and run the real scanner over it.
//
// The alternative — running the scanner against the machine's actual /proc —
// is what CI already does implicitly, and it proves almost nothing: a CI runner
// has no USB devices, so every interesting branch is skipped and the test passes
// whether or not the matching works. A fixture is the only way to exercise
// "recognises a blocker", "ignores our own process" and "survives an unreadable
// process directory" without a Linux desktop and a plugged-in Switch.

// procFixture builds a directory shaped like /proc.
type procFixture struct {
	root string
	t    *testing.T
}

func newProcFixture(t *testing.T) *procFixture {
	t.Helper()
	return &procFixture{root: t.TempDir(), t: t}
}

// addProcess creates /proc/<pid>/comm and an fd directory whose entries are
// symlinks to the given targets, mirroring how Linux exposes open descriptors.
func (f *procFixture) addProcess(pid int, name string, fdTargets ...string) {
	f.t.Helper()
	dir := filepath.Join(f.root, strconv.Itoa(pid))
	fdDir := filepath.Join(dir, "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if name != "" {
		// Linux terminates comm with a newline; include it so the test
		// exercises the trimming rather than assuming it away.
		if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(name+"\n"), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	for i, target := range fdTargets {
		link := filepath.Join(fdDir, strconv.Itoa(i))
		if err := os.Symlink(target, link); err != nil {
			f.t.Fatal(err)
		}
	}
}

// addNoise creates the non-process entries that really exist in /proc, so the
// scanner is tested against the directory it will actually meet.
func (f *procFixture) addNoise() {
	f.t.Helper()
	for _, name := range []string{"self", "net", "sys", "cpuinfo", "meminfo"} {
		p := filepath.Join(f.root, name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			f.t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.root, "uptime"), []byte("1 1\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *procFixture) scan(selfPID int) []USBClient {
	return scanProc(procScan{
		procRoot:  f.root,
		usbPrefix: "/dev/bus/usb/",
		selfPID:   selfPID,
		blockers:  knownBlockersForTest,
	})
}

// knownBlockersForTest mirrors the shape of the real table. The real one is
// Linux-tagged, so these tests use their own to stay runnable everywhere; the
// separate Linux-tagged test file checks the real table itself.
var knownBlockersForTest = map[string]string{
	"gvfsd-mtp": "GNOME's MTP backend has claimed the Switch.",
	"kiod5":     "KDE's kio-mtp worker has claimed the Switch.",
}

func findClient(clients []USBClient, pid int) (USBClient, bool) {
	for _, c := range clients {
		if c.PID == pid {
			return c, true
		}
	}
	return USBClient{}, false
}

func TestProcScanFindsOnlyProcessesHoldingUSB(t *testing.T) {
	f := newProcFixture(t)
	f.addNoise()
	f.addProcess(100, "gvfsd-mtp", "/dev/bus/usb/001/004")
	f.addProcess(200, "firefox", "/home/user/.cache/x", "socket:[12345]")
	f.addProcess(300, "bash") // no descriptors at all

	got := f.scan(999)

	if len(got) != 1 {
		t.Fatalf("expected exactly the USB holder, got %d: %+v", len(got), got)
	}
	if got[0].PID != 100 || got[0].Name != "gvfsd-mtp" {
		t.Errorf("wrong process reported: %+v", got[0])
	}
}

func TestProcScanRecognisesBlockersAndSortsThemFirst(t *testing.T) {
	f := newProcFixture(t)
	// Deliberately add the blocker with the *highest* pid, so a scanner that
	// only sorted numerically would put it last.
	f.addProcess(10, "someapp", "/dev/bus/usb/001/002")
	f.addProcess(20, "another", "/dev/bus/usb/001/003")
	f.addProcess(900, "gvfsd-mtp", "/dev/bus/usb/001/004")

	got := f.scan(999)

	if len(got) != 3 {
		t.Fatalf("expected 3 USB holders, got %d: %+v", len(got), got)
	}
	if !got[0].Blocker || got[0].Name != "gvfsd-mtp" {
		t.Errorf("the known blocker should sort first, got %+v", got[0])
	}
	if got[0].Advice == "" {
		t.Error("a known blocker must carry advice; without it the report says " +
			"something is wrong but not what to do")
	}
	// The remaining two are unknown holders and should stay in pid order.
	if got[1].PID != 10 || got[2].PID != 20 {
		t.Errorf("unknown holders lost pid ordering: %d then %d", got[1].PID, got[2].PID)
	}
	for _, c := range got[1:] {
		if c.Blocker || c.Known {
			t.Errorf("process %q was wrongly classified as a known blocker", c.Name)
		}
	}
}

func TestProcScanNeverBlamesOurselves(t *testing.T) {
	f := newProcFixture(t)
	// The nightmare case: we are running under a name that is in the blocker
	// table. Reporting ourselves would tell the user to quit the app they are
	// using to read the message.
	f.addProcess(42, "gvfsd-mtp", "/dev/bus/usb/001/004")

	got := f.scan(42)

	c, ok := findClient(got, 42)
	if !ok {
		t.Fatal("our own process should still be listed, just not as a blocker")
	}
	if !c.IsSelf {
		t.Error("our own process was not marked IsSelf")
	}
	if c.Blocker || c.Known || c.Advice != "" {
		t.Errorf("we were reported as a blocker against ourselves: %+v", c)
	}
}

func TestProcScanMatchesBlockerNamesCaseInsensitively(t *testing.T) {
	f := newProcFixture(t)
	f.addProcess(50, "GVFSD-MTP", "/dev/bus/usb/001/004")

	got := f.scan(999)

	if len(got) != 1 || !got[0].Blocker {
		t.Errorf("blocker matching should not depend on case, got %+v", got)
	}
}

func TestProcScanSurvivesUnreadableAndBrokenEntries(t *testing.T) {
	f := newProcFixture(t)
	f.addProcess(100, "gvfsd-mtp", "/dev/bus/usb/001/004")

	// A dangling symlink: the descriptor closed between listing and reading.
	// Readlink still succeeds here, so also cover a process whose fd directory
	// cannot be read at all, which is what another user's process looks like.
	f.addProcess(200, "otheruser", "/dev/bus/usb/001/009")
	locked := filepath.Join(f.root, "200", "fd")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	// A numeric directory with no fd subdirectory at all: the process exited
	// while we were walking /proc, which is entirely routine.
	if err := os.MkdirAll(filepath.Join(f.root, "300"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := f.scan(999)

	if _, ok := findClient(got, 100); !ok {
		t.Error("a readable USB holder was lost because other entries were unreadable")
	}
	if _, ok := findClient(got, 300); ok {
		t.Error("a process with no fd directory should not be reported")
	}
	// Running as root would make the chmod ineffective, so only assert the
	// exclusion when the directory really is unreadable.
	if _, err := os.ReadDir(locked); err != nil {
		if _, ok := findClient(got, 200); ok {
			t.Error("an unreadable process should be skipped, not reported")
		}
	}
}

func TestProcScanFallsBackWhenCommIsMissingOrEmpty(t *testing.T) {
	f := newProcFixture(t)
	f.addProcess(100, "", "/dev/bus/usb/001/004") // no comm file
	f.addProcess(200, "", "/dev/bus/usb/001/005")
	if err := os.WriteFile(filepath.Join(f.root, "200", "comm"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := f.scan(999)

	if len(got) != 2 {
		t.Fatalf("expected both holders, got %+v", got)
	}
	for _, c := range got {
		if c.Name == "" {
			t.Errorf("pid %d rendered a blank name; a report must never show an "+
				"empty cell where a process name belongs", c.PID)
		}
	}
}

func TestProcScanReturnsNothingWhenProcIsAbsent(t *testing.T) {
	got := scanProc(procScan{
		procRoot:  filepath.Join(t.TempDir(), "definitely-not-here"),
		usbPrefix: "/dev/bus/usb/",
		selfPID:   1,
		blockers:  knownBlockersForTest,
	})
	if got != nil {
		t.Errorf("a missing proc root should yield no clients, got %+v", got)
	}
}

func TestProcScanIgnoresNonUSBDescriptorsThatLookSimilar(t *testing.T) {
	f := newProcFixture(t)
	// Paths that share a prefix fragment but are not the USB device tree.
	f.addProcess(100, "decoy", "/dev/bus/usb-not-really/001/004")
	f.addProcess(200, "decoy2", "/var/dev/bus/usb/001/004")
	f.addProcess(300, "real", "/dev/bus/usb/001/004")

	got := f.scan(999)

	if len(got) != 1 || got[0].PID != 300 {
		t.Errorf("prefix matching is too loose: %+v", got)
	}
}
