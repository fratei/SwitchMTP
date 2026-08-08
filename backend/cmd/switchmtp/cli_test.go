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
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fratei/SwitchMTP/backend/fake"
	"github.com/fratei/SwitchMTP/backend/nxmtp"
	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// useFakeDevice points the CLI at an emulated DBI responder for the duration
// of a test.
//
// Every command below therefore runs its real code path — argument parsing,
// storage resolution, the nxmtp call, the output formatting — with only the
// USB layer replaced. That is what makes this binary verifiable on a Linux CI
// runner that has never seen a Switch.
//
// One fake device is shared across every command in a test, so that state
// written by one command is visible to the next. Each command still gets a
// fresh Client, which means the open/close lifecycle is exercised too.
func useFakeDevice(t *testing.T, opts fake.Options) *fake.Device {
	t.Helper()

	dev := fake.New(opts)
	original := openClient
	t.Cleanup(func() { openClient = original })

	openClient = func(options) (*nxmtp.Client, error) {
		// EnsureSession rather than OpenSession: the previous command closed
		// the session on its way out, but a second OpenSession on an already
		// open one would be an error.
		if err := dev.EnsureSession(); err != nil {
			return nil, err
		}
		var info mtp.DeviceInfo
		if err := dev.GetDeviceInfo(&info); err != nil {
			return nil, err
		}
		return nxmtp.NewClientWithTransport(dev, nxmtp.DeviceRef{
			VendorID:     0x057E,
			ProductID:    0x201D,
			Manufacturer: "Nintendo",
			Model:        "Nintendo Switch",
			SerialNumber: "XAW10012345678",
		}, info)
	}
	return dev
}

// captureRun runs the CLI with args and returns its stdout and exit code.
func captureRun(t *testing.T, args ...string) (string, int) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	code := run(args)

	os.Stdout = original
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out, code
}

func TestStoragesListsDBIsStoragesWithTheirAccess(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	out, code := captureRun(t, "storages")
	if code != exitOK {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}

	// The SD card is the ordinary case; an install storage is the one no
	// generic MTP client models, so both must show up correctly labelled.
	if !strings.Contains(out, "SD Card") {
		t.Errorf("expected the SD card in the listing:\n%s", out)
	}
	if !strings.Contains(out, "drop-only") {
		t.Errorf("expected an install storage marked drop-only:\n%s", out)
	}
	if !strings.Contains(out, "read/write") {
		t.Errorf("expected a read/write storage:\n%s", out)
	}
}

func TestStoragesJSONIsValidAndCarriesIDs(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	out, code := captureRun(t, "--json", "storages")
	if code != exitOK {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}

	var storages []nxmtp.Storage
	if err := json.Unmarshal([]byte(out), &storages); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(storages) == 0 {
		t.Fatal("expected at least one storage")
	}
	for _, s := range storages {
		if s.Sid == 0 {
			t.Errorf("storage %q has no id", s.DisplayName)
		}
	}
}

func TestLsResolvesAFriendlyStorageName(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	out, code := captureRun(t, "ls", "sdcard:/")
	if code != exitOK {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected a listing header:\n%s", out)
	}
	if !strings.Contains(out, "<dir>") && !strings.Contains(out, "B") {
		t.Errorf("expected entries in the listing:\n%s", out)
	}
}

func TestLsRejectsAPathWithNoStorage(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	_, code := captureRun(t, "ls", "/switch")
	if code != exitUsage {
		t.Errorf("exit %d, want %d for a malformed path", code, exitUsage)
	}
}

func TestLsOnAnUnknownStorageExplainsWhatExists(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	// The error goes to stderr; the exit code is what a script sees, and the
	// selector being wrong is a usage problem, not a device failure.
	_, code := captureRun(t, "ls", "nosuchstorage:/")
	if code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
}

// A round trip through the device is the strongest single check: it exercises
// upload, directory creation, listing and download in one path.
func TestPutThenLsThenGetRoundTrips(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	dir := t.TempDir()
	src := filepath.Join(dir, "payload.txt")
	content := strings.Repeat("switchmtp round trip\n", 64)
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if out, code := captureRun(t, "--quiet", "mkdir", "sdcard:/roundtrip"); code != exitOK {
		t.Fatalf("mkdir exit %d:\n%s", code, out)
	}
	if out, code := captureRun(t, "--quiet", "put", src, "sdcard:/roundtrip"); code != exitOK {
		t.Fatalf("put exit %d:\n%s", code, out)
	}

	out, code := captureRun(t, "ls", "sdcard:/roundtrip")
	if code != exitOK {
		t.Fatalf("ls exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "payload.txt") {
		t.Fatalf("uploaded file is not in the listing:\n%s", out)
	}

	back := t.TempDir()
	if out, code := captureRun(t, "--quiet", "get", "sdcard:/roundtrip/payload.txt", back); code != exitOK {
		t.Fatalf("get exit %d:\n%s", code, out)
	}
	got, err := os.ReadFile(filepath.Join(back, "payload.txt"))
	if err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
	if string(got) != content {
		t.Errorf("round trip corrupted the file: got %d bytes, want %d", len(got), len(content))
	}
}

func TestRmDeletesWhenConfirmationIsWaived(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	dir := t.TempDir()
	src := filepath.Join(dir, "doomed.txt")
	if err := os.WriteFile(src, []byte("delete me"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, code := captureRun(t, "--quiet", "put", src, "sdcard:/"); code != exitOK {
		t.Fatal("put failed")
	}

	if out, code := captureRun(t, "--quiet", "--yes", "rm", "sdcard:/doomed.txt"); code != exitOK {
		t.Fatalf("rm exit %d:\n%s", code, out)
	}

	out, _ := captureRun(t, "ls", "sdcard:/")
	if strings.Contains(out, "doomed.txt") {
		t.Errorf("file survived deletion:\n%s", out)
	}
}

// Deleting a storage root would wipe the SD card. It must be refused before
// the device is even opened.
func TestRmRefusesToDeleteAStorageRoot(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	if _, code := captureRun(t, "--yes", "rm", "sdcard:/"); code != exitUsage {
		t.Errorf("exit %d, want %d — deleting a storage root must be refused", code, exitUsage)
	}
}

func TestRmRefusesToMixStorages(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	if _, code := captureRun(t, "--yes", "rm", "sdcard:/a.txt", "nand:/b.txt"); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
}

// Global flags must work after the subcommand as well as before it.
//
// Go's flag package stops at the first positional argument, so before this was
// handled `switchmtp rm sdcard:/doomed.txt --yes` treated "--yes" as a second
// path to delete and failed with `"--yes" is not a device path`. That is the
// order most people type, and it was hit immediately the first time the CLI was
// driven against real hardware.
func TestGlobalFlagsAreAcceptedAfterTheSubcommand(t *testing.T) {
	for _, form := range [][]string{
		{"--quiet", "--yes", "rm", "sdcard:/doomed.txt"},
		{"rm", "sdcard:/doomed.txt", "--yes", "--quiet"},
		{"--yes", "rm", "--quiet", "sdcard:/doomed.txt"},
	} {
		t.Run(strings.Join(form, " "), func(t *testing.T) {
			_ = useFakeDevice(t, fake.Options{})

			dir := t.TempDir()
			src := filepath.Join(dir, "doomed.txt")
			if err := os.WriteFile(src, []byte("delete me"), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, code := captureRun(t, "--quiet", "put", src, "sdcard:/"); code != exitOK {
				t.Fatal("put failed")
			}

			if out, code := captureRun(t, form...); code != exitOK {
				t.Fatalf("exit %d, want %d — global flags must be accepted in any position:\n%s", code, exitOK, out)
			}

			out, _ := captureRun(t, "ls", "sdcard:/")
			if strings.Contains(out, "doomed.txt") {
				t.Errorf("file survived deletion:\n%s", out)
			}
		})
	}
}

// A "--" separator means everything after it is a path, so files whose names
// begin with a dash stay reachable rather than being read as flags.
func TestDoubleDashStopsFlagParsing(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "")

	got, err := parseInterspersed(fs, []string{"rm", "--yes", "--", "--yes"})
	if err != nil {
		t.Fatalf("parseInterspersed: %v", err)
	}
	if !*yes {
		t.Error("--yes before the separator should still have been parsed as a flag")
	}
	want := []string{"rm", "--yes"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %q, want %q — text after \"--\" must stay a positional argument", got, want)
	}
}

// Rejecting a non-installable file must not require a device: the check is
// local, so the user gets the answer whether or not a Switch is plugged in.
func TestInstallRejectsNonInstallableFilesWithoutADevice(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(bad, []byte("not a game"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Deliberately no fake device: reaching the device would be the bug.
	original := openClient
	t.Cleanup(func() { openClient = original })
	openClient = func(options) (*nxmtp.Client, error) {
		t.Error("install opened the device before validating the file type")
		return nil, os.ErrNotExist
	}

	if _, code := captureRun(t, "install", bad); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
}

func TestInstallSendsToTheInstallStorage(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	dir := t.TempDir()
	game := filepath.Join(dir, "Homebrew.nsp")
	if err := os.WriteFile(game, []byte(strings.Repeat("n", 4096)), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, code := captureRun(t, "--quiet", "install", game)
	if code != exitOK {
		t.Fatalf("install exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Installed") {
		t.Errorf("expected an install confirmation:\n%s", out)
	}
}

func TestGetRefusesToReadAWriteOnlyStorage(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	// Install storages accept files but cannot be listed or read back; asking
	// deserves the explanation rather than a raw MTP error.
	_, code := captureRun(t, "get", "sdinstall:/anything.nsp", t.TempDir())
	if code == exitOK {
		t.Error("reading an install storage should fail")
	}
}

func TestGetRejectsADeviceDestination(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	if _, code := captureRun(t, "get", "sdcard:/a.txt", "sdcard:/b.txt"); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
}

func TestMvRenamesInPlace(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	dir := t.TempDir()
	src := filepath.Join(dir, "before.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, code := captureRun(t, "--quiet", "put", src, "sdcard:/"); code != exitOK {
		t.Fatal("put failed")
	}

	if out, code := captureRun(t, "--quiet", "mv", "sdcard:/before.txt", "after.txt"); code != exitOK {
		t.Fatalf("mv exit %d:\n%s", code, out)
	}

	out, _ := captureRun(t, "ls", "sdcard:/")
	if !strings.Contains(out, "after.txt") || strings.Contains(out, "before.txt") {
		t.Errorf("rename did not take effect:\n%s", out)
	}
}

// Moving between directories is a different MTP operation. Silently doing the
// wrong one would be worse than refusing.
func TestMvRefusesToMoveBetweenDirectories(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{})

	if _, code := captureRun(t, "mv", "sdcard:/a/x.txt", "sdcard:/b/x.txt"); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
	if _, code := captureRun(t, "mv", "sdcard:/a/x.txt", "sub/dir.txt"); code != exitUsage {
		t.Errorf("exit %d, want %d for a name containing a separator", code, exitUsage)
	}
}

func TestDisconnectedDeviceIsNotReportedAsAUsageError(t *testing.T) {
	_ = useFakeDevice(t, fake.Options{Disconnected: true})

	_, code := captureRun(t, "ls", "sdcard:/")
	if code == exitOK {
		t.Fatal("a disconnected device should fail")
	}
	if code == exitUsage {
		t.Error("a disconnected device is not a usage error")
	}
}

func TestInfoReportsCapabilitiesIncludingUnsupportedOnes(t *testing.T) {
	// NoPropList forces the fallback path, so the report must say the
	// operation is unavailable rather than claiming everything works.
	_ = useFakeDevice(t, fake.Options{NoPropList: true, NoSetPropValue: true})

	out, code := captureRun(t, "info")
	if code != exitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "GetObjectPropList") {
		t.Fatalf("expected the capability table:\n%s", out)
	}
	if !strings.Contains(out, "rename=no") {
		t.Errorf("expected rename to be reported as unavailable:\n%s", out)
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	if _, code := captureRun(t, "frobnicate"); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
	if _, code := captureRun(t); code != exitUsage {
		t.Errorf("exit %d for no arguments, want %d", code, exitUsage)
	}
}

func TestHelpExitsSuccessfully(t *testing.T) {
	out, code := captureRun(t, "help")
	if code != exitOK {
		t.Errorf("exit %d, want %d", code, exitOK)
	}
	if !strings.Contains(out, "install") {
		t.Errorf("help should list the commands:\n%s", out)
	}
}

func TestVersionJSONIsMachineReadable(t *testing.T) {
	out, code := captureRun(t, "--json", "version")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var v map[string]string
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if v["version"] == "" || v["os"] == "" {
		t.Errorf("version JSON is missing fields: %v", v)
	}
}

// doctor must work with no device present — that is precisely when it is
// needed.
func TestDoctorRunsWithoutADevice(t *testing.T) {
	out, code := captureRun(t, "doctor")
	if code != exitOK {
		t.Fatalf("doctor exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "SwitchMTP") {
		t.Errorf("expected a diagnostic report:\n%s", out)
	}
}

func TestDoctorJSONIsMachineReadable(t *testing.T) {
	out, code := captureRun(t, "--json", "doctor")
	if code != exitOK {
		t.Fatalf("doctor exit %d", code)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	for _, key := range []string{"platform", "arch", "summary", "advice"} {
		if _, ok := d[key]; !ok {
			t.Errorf("diagnostics JSON is missing %q", key)
		}
	}
}
