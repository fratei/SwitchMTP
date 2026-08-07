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
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

// remotePath is a location on the device: a storage selector plus a path
// within it.
//
// The wire form is "<storage>:<path>". The storage part accepts a numeric
// storage id (65537), a kind name (sdcard), or a case-insensitive prefix of
// the storage's display name (sd). Numeric ids are what the app and the MTP
// protocol actually use; the names exist because nobody remembers 65537.
type remotePath struct {
	// Selector is the storage part as the user wrote it, kept for error
	// messages.
	Selector string
	// Path is the path within the storage, always absolute and slash-rooted.
	Path string
}

// parseRemotePath splits "<storage>:<path>".
//
// A missing path means the storage root. The separator is the *first* colon:
// paths may legitimately contain colons, storage selectors may not.
func parseRemotePath(arg string) (remotePath, error) {
	i := strings.Index(arg, ":")
	if i < 0 {
		return remotePath{}, usagef("%q is not a device path: expected <storage>:<path>, for example 65537:/games or sdcard:/", arg)
	}
	sel := strings.TrimSpace(arg[:i])
	if sel == "" {
		return remotePath{}, usagef("%q is missing a storage: expected <storage>:<path>, for example sdcard:/games", arg)
	}
	return remotePath{Selector: sel, Path: normaliseDevicePath(arg[i+1:])}, nil
}

// isRemotePath reports whether an argument looks like a device path rather
// than a local one.
//
// The ambiguity is real on Windows, where "C:\games" has the same shape. That
// is why a single letter never counts as a storage selector: no storage kind
// or numeric id is one character long.
func isRemotePath(arg string) bool {
	i := strings.Index(arg, ":")
	return i > 1
}

// normaliseDevicePath makes a device path absolute and removes redundant
// separators, so "games/", "/games" and "//games/." all agree.
func normaliseDevicePath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

// storageAliases maps friendly names onto the kinds nxmtp classifies storages
// into. Several spellings map to the same kind because users type what they
// see on the console, not what we called it internally.
var storageAliases = map[string]nxmtp.StorageKind{
	"sdcard":         nxmtp.KindSDCard,
	"sd":             nxmtp.KindSDCard,
	"microsd":        nxmtp.KindSDCard,
	"nand":           nxmtp.KindNandUser,
	"nanduser":       nxmtp.KindNandUser,
	"user":           nxmtp.KindNandUser,
	"nandsystem":     nxmtp.KindNandSystem,
	"system":         nxmtp.KindNandSystem,
	"games":          nxmtp.KindInstalledGames,
	"installedgames": nxmtp.KindInstalledGames,
	"sdinstall":      nxmtp.KindSDInstall,
	"installsd":      nxmtp.KindSDInstall,
	"nandinstall":    nxmtp.KindNandInstall,
	"installnand":    nxmtp.KindNandInstall,
	"saves":          nxmtp.KindSaves,
	"save":           nxmtp.KindSaves,
	"album":          nxmtp.KindAlbum,
	"screenshots":    nxmtp.KindAlbum,
	"gamecard":       nxmtp.KindGamecard,
	"cartridge":      nxmtp.KindGamecard,
}

// resolveStorage turns a selector into one of the device's storages.
//
// Resolution is ordered most-precise-first so that an exact match always wins
// over a fuzzy one: numeric id, then kind alias, then exact display name, then
// unique display-name prefix. Ambiguity is an error rather than a guess —
// picking the wrong storage here could mean writing to NAND instead of the SD
// card.
func resolveStorage(storages []nxmtp.Storage, selector string) (*nxmtp.Storage, error) {
	if len(storages) == 0 {
		return nil, fmt.Errorf("the device reported no storages")
	}
	norm := strings.ToLower(strings.TrimSpace(selector))

	if id, err := strconv.ParseUint(norm, 10, 32); err == nil {
		for i := range storages {
			if storages[i].Sid == uint32(id) {
				return &storages[i], nil
			}
		}
		return nil, fmt.Errorf("no storage with id %d on this device%s", id, storageHint(storages))
	}

	if kind, ok := storageAliases[strings.ReplaceAll(norm, " ", "")]; ok {
		var matches []*nxmtp.Storage
		for i := range storages {
			if storages[i].Kind == kind {
				matches = append(matches, &storages[i])
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return nil, ambiguous(selector, matches)
		}
		return nil, fmt.Errorf("this device has no %q storage%s", selector, storageHint(storages))
	}

	for i := range storages {
		if strings.EqualFold(storages[i].DisplayName, selector) {
			return &storages[i], nil
		}
	}

	var prefix []*nxmtp.Storage
	for i := range storages {
		if strings.HasPrefix(strings.ToLower(storages[i].DisplayName), norm) {
			prefix = append(prefix, &storages[i])
		}
	}
	switch len(prefix) {
	case 1:
		return prefix[0], nil
	case 0:
		return nil, fmt.Errorf("no storage matches %q%s", selector, storageHint(storages))
	default:
		return nil, ambiguous(selector, prefix)
	}
}

func ambiguous(selector string, matches []*nxmtp.Storage) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d storages; use the id instead:", selector, len(matches))
	for _, m := range matches {
		fmt.Fprintf(&b, "\n  %d  %s", m.Sid, m.DisplayName)
	}
	return fmt.Errorf("%s", b.String())
}

func storageHint(storages []nxmtp.Storage) string {
	var b strings.Builder
	b.WriteString("\n\nAvailable storages:")
	for _, s := range storages {
		fmt.Fprintf(&b, "\n  %-7d %s", s.Sid, s.DisplayName)
	}
	return b.String()
}
