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
	"strings"
	"testing"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

func TestParseRemotePathSplitsOnTheFirstColon(t *testing.T) {
	cases := []struct {
		in       string
		selector string
		path     string
	}{
		{"sdcard:/games", "sdcard", "/games"},
		{"65537:/switch/atmosphere", "65537", "/switch/atmosphere"},
		{"sd:", "sd", "/"},
		{"sd:/", "sd", "/"},
		{"sdcard:games", "sdcard", "/games"},
		{"sdcard:/games/", "sdcard", "/games"},
		{"sdcard://games//sub/", "sdcard", "/games/sub"},
		{"sdcard:/games/.", "sdcard", "/games"},
		// A colon inside the path must stay in the path: the split is on the
		// first colon only.
		{"sdcard:/Album/2024:01.jpg", "sdcard", "/Album/2024:01.jpg"},
		{" sdcard :/games", "sdcard", "/games"},
	}
	for _, c := range cases {
		got, err := parseRemotePath(c.in)
		if err != nil {
			t.Fatalf("parseRemotePath(%q): %v", c.in, err)
		}
		if got.Selector != c.selector || got.Path != c.path {
			t.Errorf("parseRemotePath(%q) = {%q, %q}, want {%q, %q}",
				c.in, got.Selector, got.Path, c.selector, c.path)
		}
	}
}

func TestParseRemotePathRejectsMalformedInput(t *testing.T) {
	for _, in := range []string{"sdcard", "/local/path", ":/games", "  :x"} {
		if _, err := parseRemotePath(in); err == nil {
			t.Errorf("parseRemotePath(%q) should have failed", in)
		}
	}
}

// A Windows drive letter has the same shape as a device path. Treating "C:\x"
// as a device path would send a download to the wrong place, so a single
// leading character never counts as a storage selector.
func TestIsRemotePathIgnoresWindowsDriveLetters(t *testing.T) {
	remote := []string{"sd:/games", "65537:/", "sdcard:x"}
	local := []string{"C:\\games", "d:/games", "./relative", "/absolute", "plain.nsp"}

	for _, s := range remote {
		if !isRemotePath(s) {
			t.Errorf("isRemotePath(%q) = false, want true", s)
		}
	}
	for _, s := range local {
		if isRemotePath(s) {
			t.Errorf("isRemotePath(%q) = true, want false", s)
		}
	}
}

func testStorages() []nxmtp.Storage {
	return []nxmtp.Storage{
		{Sid: 65537, DisplayName: "SD Card", Kind: nxmtp.KindSDCard},
		{Sid: 1, DisplayName: "System memory", Kind: nxmtp.KindNandUser},
		{Sid: 2, DisplayName: "SD Card Install", Kind: nxmtp.KindSDInstall},
		{Sid: 3, DisplayName: "Installed games", Kind: nxmtp.KindInstalledGames},
	}
}

func TestResolveStorageByNumericID(t *testing.T) {
	st, err := resolveStorage(testStorages(), "65537")
	if err != nil {
		t.Fatalf("resolveStorage: %v", err)
	}
	if st.Sid != 65537 {
		t.Errorf("got storage %d, want 65537", st.Sid)
	}
}

func TestResolveStorageByAlias(t *testing.T) {
	cases := map[string]uint32{
		"sdcard":    65537,
		"sd":        65537,
		"SD":        65537,
		"MicroSD":   65537,
		"nand":      1,
		"sdinstall": 2,
		"games":     3,
	}
	for selector, want := range cases {
		st, err := resolveStorage(testStorages(), selector)
		if err != nil {
			t.Fatalf("resolveStorage(%q): %v", selector, err)
		}
		if st.Sid != want {
			t.Errorf("resolveStorage(%q) = %d, want %d", selector, st.Sid, want)
		}
	}
}

// "SD Card" and "SD Card Install" share a prefix. An exact display-name match
// must win outright, because resolving it to the install storage would turn a
// browse into a silent write attempt.
func TestResolveStorageExactNameBeatsPrefix(t *testing.T) {
	st, err := resolveStorage(testStorages(), "SD Card")
	if err != nil {
		t.Fatalf("resolveStorage: %v", err)
	}
	if st.Sid != 65537 {
		t.Errorf("got %d (%s), want 65537", st.Sid, st.DisplayName)
	}
}

func TestResolveStorageAmbiguousPrefixIsAnError(t *testing.T) {
	// "SD C" prefixes both "SD Card" and "SD Card Install" and is not an
	// alias, so it must refuse rather than pick one.
	_, err := resolveStorage(testStorages(), "SD C")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}
	if !strings.Contains(err.Error(), "65537") || !strings.Contains(err.Error(), "2") {
		t.Errorf("ambiguity error should list the candidate ids, got: %v", err)
	}
}

func TestResolveStorageUnknownSelectorListsWhatExists(t *testing.T) {
	_, err := resolveStorage(testStorages(), "nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The error is the user's only way to discover the right name, so it must
	// carry the available storages.
	for _, want := range []string{"65537", "SD Card", "System memory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestResolveStorageMissingKindIsAnError(t *testing.T) {
	only := []nxmtp.Storage{{Sid: 65537, DisplayName: "SD Card", Kind: nxmtp.KindSDCard}}
	if _, err := resolveStorage(only, "nand"); err == nil {
		t.Fatal("expected an error for a kind the device does not expose")
	}
	if _, err := resolveStorage(nil, "sdcard"); err == nil {
		t.Fatal("expected an error when the device reports no storages")
	}
}

func TestNormaliseDevicePathNeverEscapesTheRoot(t *testing.T) {
	// path.Clean resolves "..", so a traversal attempt collapses to the root
	// rather than producing a path with ".." still in it.
	for _, in := range []string{"/..", "/../..", "/a/../..", ".."} {
		if got := normaliseDevicePath(in); strings.Contains(got, "..") {
			t.Errorf("normaliseDevicePath(%q) = %q, still contains \"..\"", in, got)
		}
	}
}
