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

package nxmtp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fratei/SwitchMTP/backend/fake"
	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

// writeNSP creates a fake installable file of the given size.
func writeNSP(t *testing.T, dir, name string, size int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// TestInstallReportsInstallingPhase covers the window that made the app look
// hung: DBI keeps working after the last byte is handed over, and MTP has no
// completion event for it. If the backend stays on "transferring" the UI shows
// a full progress bar and a stalled byte counter for however long the console
// takes to commit.
func TestInstallReportsInstallingPhase(t *testing.T) {
	c, dev := newClient(t, fake.Options{})
	dir := t.TempDir()
	src := writeNSP(t, dir, "title.nsp", 4096)

	install := storageByKind(t, c, nxmtp.KindSDInstall)

	var statuses []nxmtp.TransferStatus
	var installNote string
	_, err := c.Upload(nxmtp.UploadRequest{
		StorageID:   install.Sid,
		Sources:     []string{src},
		Destination: "/",
	}, nil, func(p nxmtp.Progress) {
		if len(statuses) == 0 || statuses[len(statuses)-1] != p.Status {
			statuses = append(statuses, p.Status)
		}
		if p.Status == nxmtp.StatusInstalling && installNote == "" {
			installNote = p.Note
		}
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	if len(dev.Installed) != 1 {
		t.Fatalf("expected 1 installed object, got %v", dev.Installed)
	}

	want := []nxmtp.TransferStatus{
		nxmtp.StatusTransferring,
		nxmtp.StatusInstalling,
		nxmtp.StatusCompleted,
	}
	if len(statuses) != len(want) {
		t.Fatalf("status sequence = %v, want %v", statuses, want)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("status sequence = %v, want %v", statuses, want)
		}
	}
	if installNote == "" {
		t.Error("the installing phase must carry a note telling the user to watch the console")
	}
}

// TestInstallSerialisesAndReportsEachTitle checks a multi-file install reports
// each title separately rather than presenting the batch as one opaque blob,
// and that the status returns to "transferring" when the next title starts.
func TestInstallSerialisesAndReportsEachTitle(t *testing.T) {
	c, dev := newClient(t, fake.Options{})
	dir := t.TempDir()
	first := writeNSP(t, dir, "first.nsp", 2048)
	second := writeNSP(t, dir, "second.nsp", 2048)

	install := storageByKind(t, c, nxmtp.KindSDInstall)

	type event struct {
		name   string
		status nxmtp.TransferStatus
	}
	var events []event
	_, err := c.Upload(nxmtp.UploadRequest{
		StorageID:   install.Sid,
		Sources:     []string{first, second},
		Destination: "/",
	}, nil, func(p nxmtp.Progress) {
		e := event{p.Name, p.Status}
		if len(events) == 0 || events[len(events)-1] != e {
			events = append(events, e)
		}
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	if len(dev.Installed) != 2 {
		t.Fatalf("expected 2 installed objects, got %v", dev.Installed)
	}

	// The second title must be announced as transferring, not inherit the
	// first one's "installing" state.
	var sawSecondTransferring bool
	for _, e := range events {
		if e.name == "second.nsp" && e.status == nxmtp.StatusTransferring {
			sawSecondTransferring = true
		}
	}
	if !sawSecondTransferring {
		t.Errorf("second title never reported as transferring; events = %v", events)
	}

	// And the first must have reached "installing" before the second began.
	firstInstalling := -1
	secondStart := -1
	for i, e := range events {
		if firstInstalling < 0 && e.name == "first.nsp" && e.status == nxmtp.StatusInstalling {
			firstInstalling = i
		}
		if secondStart < 0 && e.name == "second.nsp" {
			secondStart = i
		}
	}
	if firstInstalling < 0 || secondStart < 0 || firstInstalling > secondStart {
		t.Errorf("installs not serialised in report order; events = %v", events)
	}
}

// TestInstallFailureReportsFailedStatus makes sure a rejected install ends the
// transfer rather than leaving the UI waiting forever.
func TestInstallFailureReportsFailedStatus(t *testing.T) {
	c, _ := newClient(t, fake.Options{})
	dir := t.TempDir()
	src := writeNSP(t, dir, "notes.txt", 16)

	install := storageByKind(t, c, nxmtp.KindSDInstall)

	_, err := c.Upload(nxmtp.UploadRequest{
		StorageID:   install.Sid,
		Sources:     []string{src},
		Destination: "/",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected a non-installable file to be rejected")
	}
	if nxmtp.KindOf(err) != nxmtp.KindInvalidInput {
		t.Fatalf("expected KindInvalidInput, got %v (%v)", nxmtp.KindOf(err), err)
	}
}

// TestSingleInstallWaitsForDeviceReady pins the serialisation invariant for
// single-file installs.
//
// The app installs one file per call so it can queue titles across calls. That
// makes the "is this the last item?" guard that used to wrap waitForDeviceReady
// always false, which silently turned the readiness wait -- and the whole
// reason installs are serialised -- into dead code. Nothing in the app could
// have detected that: it would simply have started handing DBI the next title
// while it was still committing the previous one.
//
// GetStorageIDs is the readiness probe, so counting it proves the wait ran.
func TestSingleInstallWaitsForDeviceReady(t *testing.T) {
	c, dev := newClient(t, fake.Options{})
	dir := t.TempDir()
	src := writeNSP(t, dir, "solo.nsp", 2048)

	install := storageByKind(t, c, nxmtp.KindSDInstall)

	before := dev.Calls["GetStorageIDs"]
	if _, err := c.Upload(nxmtp.UploadRequest{
		StorageID:   install.Sid,
		Sources:     []string{src},
		Destination: "/",
	}, nil, nil); err != nil {
		t.Fatalf("install: %v", err)
	}

	if got := dev.Calls["GetStorageIDs"] - before; got < 1 {
		t.Fatalf("a single-file install must still wait for the console to answer "+
			"before returning; GetStorageIDs probes during upload = %d, want >= 1", got)
	}
}
