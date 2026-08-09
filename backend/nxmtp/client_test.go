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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ganeshrvel/go-mtpfs/mtp"

	"github.com/fratei/SwitchMTP/backend/fake"
	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

// newClient wires a fake DBI device to a real Client.
func newClient(t *testing.T, opts fake.Options) (*nxmtp.Client, *fake.Device) {
	t.Helper()

	dev := fake.New(opts)
	if err := dev.OpenSession(); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	var info mtp.DeviceInfo
	if err := dev.GetDeviceInfo(&info); err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	ref := nxmtp.DeviceRef{
		VendorID:     0x057E,
		ProductID:    0x201D,
		Manufacturer: "Nintendo",
		Model:        "Nintendo Switch",
		SerialNumber: "XAW10012345678",
	}
	c, err := nxmtp.NewClientWithTransport(dev, ref, info)
	if err != nil {
		t.Fatalf("NewClientWithTransport: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, dev
}

func storageByKind(t *testing.T, c *nxmtp.Client, kind nxmtp.StorageKind) nxmtp.Storage {
	t.Helper()
	sts, err := c.Storages()
	if err != nil {
		t.Fatalf("Storages: %v", err)
	}
	for _, s := range sts {
		if s.Kind == kind {
			return s
		}
	}
	t.Fatalf("no storage of kind %q among %d storages", kind, len(sts))
	return nxmtp.Storage{}
}

// --- storage classification ---------------------------------------------

// DBI's nine storages must be classified correctly, because the capability
// flags derived from them are what stop the UI offering writes to a read-only
// storage or browsing into a write-only one.
func TestStorageClassification(t *testing.T) {
	c, _ := newClient(t, fake.Options{})

	sts, err := c.Storages()
	if err != nil {
		t.Fatalf("Storages: %v", err)
	}
	if len(sts) != 9 {
		t.Fatalf("expected 9 storages, got %d", len(sts))
	}

	byKind := map[nxmtp.StorageKind]nxmtp.Storage{}
	for _, s := range sts {
		byKind[s.Kind] = s
	}

	for _, want := range []nxmtp.StorageKind{
		nxmtp.KindSDCard, nxmtp.KindNandUser, nxmtp.KindNandSystem,
		nxmtp.KindInstalledGames, nxmtp.KindSDInstall, nxmtp.KindNandInstall,
		nxmtp.KindSaves, nxmtp.KindAlbum, nxmtp.KindGamecard,
	} {
		if _, ok := byKind[want]; !ok {
			t.Errorf("storage kind %q not classified", want)
		}
	}

	sd := byKind[nxmtp.KindSDCard]
	if !sd.Capabilities.Browse || !sd.Capabilities.Read || !sd.Capabilities.Write ||
		!sd.Capabilities.Delete {
		t.Errorf("SD Card should be fully capable, got %+v", sd.Capabilities)
	}

	// An install storage is a drop target. Presenting it as browsable is the
	// single most confusing thing the UI could do.
	for _, k := range []nxmtp.StorageKind{nxmtp.KindSDInstall, nxmtp.KindNandInstall} {
		s := byKind[k]
		if s.Capabilities.Browse || s.Capabilities.Read {
			t.Errorf("%s must not be browsable/readable, got %+v", k, s.Capabilities)
		}
		if !s.Capabilities.InstallTarget {
			t.Errorf("%s must be an install target", k)
		}
	}

	for _, k := range []nxmtp.StorageKind{
		nxmtp.KindNandUser, nxmtp.KindNandSystem,
		nxmtp.KindInstalledGames, nxmtp.KindAlbum, nxmtp.KindGamecard,
	} {
		s := byKind[k]
		if s.Capabilities.Write || s.Capabilities.Delete {
			t.Errorf("%s must be read-only, got %+v", k, s.Capabilities)
		}
	}

	// Virtual storages report zero capacity; a size shown from that would be a
	// lie, so it must be flagged unreliable.
	if games := byKind[nxmtp.KindInstalledGames]; !games.Virtual || games.SizeReliable {
		t.Errorf("Installed games should be virtual with unreliable size, got %+v", games)
	}
}

// --- listing ------------------------------------------------------------

func TestWalkListsDirectory(t *testing.T) {
	c, _ := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	files, err := c.Walk(nxmtp.WalkOptions{StorageID: sd.Sid, FullPath: "/"})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	names := map[string]nxmtp.FileInfo{}
	for _, f := range files {
		names[f.Name] = f
	}
	for _, want := range []string{"switch", "games", "readme.txt"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing %q in root listing; got %v", want, keys(names))
		}
	}
	if !names["switch"].IsFolder {
		t.Error("switch should be a folder")
	}
	if got := names["readme.txt"].Size; got != int64(len("hello from the fake switch")) {
		t.Errorf("readme.txt size = %d", got)
	}
	if names["readme.txt"].Extension != "txt" {
		t.Errorf("extension = %q, want txt", names["readme.txt"].Extension)
	}
}

// The listing must survive a device that refuses the property operations,
// because that is the failure mode that broke go-mtpx outright.
func TestWalkFallsBackWhenPropListUnsupported(t *testing.T) {
	for name, opts := range map[string]fake.Options{
		"unadvertised":         {NoPropList: true, NoPropValue: true},
		"secretly unsupported": {SecretlyUnsupported: true},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := newClient(t, opts)
			sd := storageByKind(t, c, nxmtp.KindSDCard)

			files, err := c.Walk(nxmtp.WalkOptions{StorageID: sd.Sid, FullPath: "/"})
			if err != nil {
				t.Fatalf("Walk: %v", err)
			}
			if len(files) != 3 {
				t.Fatalf("expected 3 entries, got %d", len(files))
			}
		})
	}
}

func TestWalkRecursive(t *testing.T) {
	c, _ := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	files, err := c.Walk(nxmtp.WalkOptions{StorageID: sd.Sid, FullPath: "/", Recursive: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var found bool
	for _, f := range files {
		if f.Path == "/switch/prod.keys" {
			found = true
		}
	}
	if !found {
		t.Errorf("recursive walk missed /switch/prod.keys; got %d entries", len(files))
	}
}

// A filename outside the basic multilingual plane arrives as a UTF-16
// surrogate pair. Mishandling it corrupts the name and makes the file
// unaddressable.
func TestWalkHandlesSurrogatePairFilenames(t *testing.T) {
	c, _ := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	files, err := c.Walk(nxmtp.WalkOptions{StorageID: sd.Sid, FullPath: "/games"})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Name, "\U0001F600") {
			return
		}
	}
	t.Errorf("surrogate-pair filename not preserved; got %v", nameList(files))
}

// --- >4 GiB sizes -------------------------------------------------------

// ObjectInfo carries a 32-bit size. For a 6 GiB dump it saturates, and the
// true size is only available from GetObjectPropValue.
func TestLargeFileSizeFromPropValue(t *testing.T) {
	c, _ := newClient(t, fake.Options{})
	games := storageByKind(t, c, nxmtp.KindInstalledGames)

	files, err := c.Walk(nxmtp.WalkOptions{StorageID: games.Sid, FullPath: "/"})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var big *nxmtp.FileInfo
	for i := range files {
		if strings.HasPrefix(files[i].Name, "0100000000010000") {
			big = &files[i]
		}
	}
	if big == nil {
		t.Fatalf("large dump not listed; got %v", nameList(files))
	}
	if want := int64(6) << 30; big.Size != want {
		t.Errorf("size = %d, want %d", big.Size, want)
	}
	if big.SizeUnknown {
		t.Error("size should be known when GetObjectPropValue works")
	}
}

// When the device cannot report the true size, reporting the saturated 32-bit
// value would tell the user a 6 GiB file is 4 GiB. Unknown is the honest answer.
func TestLargeFileSizeUnknownWithoutPropValue(t *testing.T) {
	c, _ := newClient(t, fake.Options{NoPropList: true, NoPropValue: true})
	games := storageByKind(t, c, nxmtp.KindInstalledGames)

	files, err := c.Walk(nxmtp.WalkOptions{StorageID: games.Sid, FullPath: "/"})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name, "0100000000010000") {
			if !f.SizeUnknown {
				t.Errorf("expected sizeUnknown, got size=%d", f.Size)
			}
			if f.Size == 0xFFFFFFFF {
				t.Error("must not report the saturated 32-bit size as truth")
			}
			return
		}
	}
	t.Fatal("large dump not listed")
}

// --- transfers ----------------------------------------------------------

func TestDownloadWritesFile(t *testing.T) {
	c, _ := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)
	dest := t.TempDir()

	var progressSeen int
	summary, err := c.Download(
		nxmtp.DownloadRequest{StorageID: sd.Sid, Sources: []string{"/readme.txt"}, Destination: dest},
		nil,
		func(nxmtp.Progress) { progressSeen++ },
	)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if summary == nil || summary.TotalFiles != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	got, err := os.ReadFile(filepath.Join(dest, "readme.txt"))
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(got) != "hello from the fake switch" {
		t.Errorf("content = %q", got)
	}
	if progressSeen == 0 {
		t.Error("no progress callbacks fired")
	}
	// A partial download must never be left behind looking complete.
	if entries, _ := os.ReadDir(dest); len(entries) != 1 {
		t.Errorf("expected exactly one file, got %d", len(entries))
	}
}

func TestUploadThenReadBack(t *testing.T) {
	c, dev := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	src := filepath.Join(t.TempDir(), "upload.bin")
	payload := strings.Repeat("switch", 2000)
	if err := os.WriteFile(src, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Upload(
		nxmtp.UploadRequest{StorageID: sd.Sid, Sources: []string{src}, Destination: "/"},
		nil, nil,
	); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	got, ok := dev.Data(fake.SidSDCard, "/upload.bin")
	if !ok {
		t.Fatalf("uploaded file not found; tree = %v", dev.Tree())
	}
	if string(got) != payload {
		t.Errorf("uploaded %d bytes, device has %d", len(payload), len(got))
	}
}

// Writing to a read-only storage must fail before any bytes move, with an
// error the UI can explain.
func TestUploadToReadOnlyStorageRejected(t *testing.T) {
	c, _ := newClient(t, fake.Options{})
	album := storageByKind(t, c, nxmtp.KindAlbum)

	src := filepath.Join(t.TempDir(), "x.bin")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := c.Upload(
		nxmtp.UploadRequest{StorageID: album.Sid, Sources: []string{src}, Destination: "/"},
		nil, nil,
	)
	if err == nil {
		t.Fatal("expected an error uploading to a read-only storage")
	}
	// The device is writable in principle; this particular storage is not, and
	// saying so is more useful than a generic "unsupported".
	if got := nxmtp.KindOf(err); got != nxmtp.KindReadOnly {
		t.Errorf("kind = %q, want %q (%v)", got, nxmtp.KindReadOnly, err)
	}
}

// --- mutation -----------------------------------------------------------

func TestMakeDirectoryAndDelete(t *testing.T) {
	c, dev := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	if err := c.MakeDirectory(sd.Sid, "/newdir"); err != nil {
		t.Fatalf("MakeDirectory: %v", err)
	}
	if dev.Find(fake.SidSDCard, "/newdir") == nil {
		t.Fatalf("directory not created; tree = %v", dev.Tree())
	}
	if err := c.Delete(sd.Sid, []string{"/newdir"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if dev.Find(fake.SidSDCard, "/newdir") != nil {
		t.Error("directory not deleted")
	}
}

// Delete must recurse: MTP refuses to remove a non-empty association, so a
// naive single DeleteObject would fail on any real folder.
func TestDeleteRecursesIntoDirectories(t *testing.T) {
	c, dev := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	if err := c.Delete(sd.Sid, []string{"/switch"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if dev.Find(fake.SidSDCard, "/switch") != nil {
		t.Error("directory still present")
	}
	if dev.Find(fake.SidSDCard, "/switch/prod.keys") != nil {
		t.Error("child still present")
	}
}

func TestRenameSucceedsWhenSupported(t *testing.T) {
	c, dev := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	if err := c.Rename(sd.Sid, "/readme.txt", "notes.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if dev.Find(fake.SidSDCard, "/notes.txt") == nil {
		t.Errorf("rename did not take effect; tree = %v", dev.Tree())
	}
}

// Rename is optional in MTP. If the device cannot do it the app must say so
// rather than silently appearing to succeed.
func TestRenameReportsUnsupported(t *testing.T) {
	c, _ := newClient(t, fake.Options{NoSetPropValue: true})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	err := c.Rename(sd.Sid, "/readme.txt", "notes.txt")
	if err == nil {
		t.Fatal("expected rename to fail")
	}
	if !nxmtp.IsUnsupported(err) {
		t.Errorf("kind = %q, want operationUnsupported (%v)", nxmtp.KindOf(err), err)
	}
}

// --- error classification -----------------------------------------------

func TestDisconnectedDeviceClassified(t *testing.T) {
	c, dev := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	dev.SetDisconnected(true)

	_, err := c.Walk(nxmtp.WalkOptions{StorageID: sd.Sid, FullPath: "/"})
	if err == nil {
		t.Fatal("expected an error after disconnect")
	}
	if !nxmtp.IsDisconnected(err) {
		t.Errorf("kind = %q, want a disconnect (%v)", nxmtp.KindOf(err), err)
	}
}

func TestFileExists(t *testing.T) {
	c, _ := newClient(t, fake.Options{})
	sd := storageByKind(t, c, nxmtp.KindSDCard)

	res, err := c.FileExists(sd.Sid, []string{"/readme.txt", "/nope.txt"})
	if err != nil {
		t.Fatalf("FileExists: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results", len(res))
	}
	if !res[0].Exists {
		t.Error("/readme.txt should exist")
	}
	if res[1].Exists {
		t.Error("/nope.txt should not exist")
	}
}

// --- helpers ------------------------------------------------------------

func keys(m map[string]nxmtp.FileInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func nameList(files []nxmtp.FileInfo) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Name)
	}
	return out
}

// TestClosedHandleClassifiedAsDisconnect pins the wording the MTP engine uses
// when its USB handle has been closed underneath it.
//
// This is what a Switch that goes away mid-transfer actually leaves behind, and
// it does not contain the word "disconnected". Reading it as merely unknown left
// the dead session cached, so every reconnect was handed the same corpse and the
// app could not reach the console again until it was relaunched.
func TestClosedHandleClassifiedAsDisconnect(t *testing.T) {
	// Verbatim from mtp.RunTransaction, via GetStorageIDs.
	err := errors.New("mtp: cannot run operation GetStorageIDs, device is not open")
	if !nxmtp.IsDisconnected(err) {
		t.Errorf("IsDisconnected(%q) = false, want true", err)
	}
}

// TestHealthyErrorsAreNotDisconnects guards the needle above from being so broad
// that ordinary failures start evicting a working session.
func TestHealthyErrorsAreNotDisconnects(t *testing.T) {
	for _, msg := range []string{
		"mtp: GetObjectHandles failed: store is full",
		"could not open the file for reading",
		"permission denied",
		"the device is busy",
	} {
		if nxmtp.IsDisconnected(errors.New(msg)) {
			t.Errorf("IsDisconnected(%q) = true, want false", msg)
		}
	}
}

// TestValidateDetectsDeadSession covers the gap Details() leaves open.
//
// Details() answers from cached device info and never touches the wire, so it
// keeps reporting a healthy device long after the handle has closed. That is why
// an overnight reconnect loop logged "initializing" as a success and then failed
// fetching storages a moment later. Validate has to disagree with Details here.
func TestValidateDetectsDeadSession(t *testing.T) {
	c, dev := newClient(t, fake.Options{})

	if err := c.Validate(); err != nil {
		t.Fatalf("Validate on a healthy session: %v", err)
	}

	dev.SetDisconnected(true)

	if c.Details() == nil {
		t.Fatal("Details returned nil; the cached-info premise of this test is wrong")
	}
	if err := c.Validate(); err == nil {
		t.Error("Validate = nil on a dead session, want an error")
	}
}
