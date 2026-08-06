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

package nxmtp

import (
	"strings"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// StorageKind identifies what a storage actually is, which on DBI matters a
// great deal: two of its storages are write-only install triggers that must
// never be browsed, and one is a virtual filesystem of generated NSP dumps.
type StorageKind string

const (
	KindSDCard         StorageKind = "sdCard"
	KindNandUser       StorageKind = "nandUser"
	KindNandSystem     StorageKind = "nandSystem"
	KindInstalledGames StorageKind = "installedGames"
	KindSDInstall      StorageKind = "sdInstall"
	KindNandInstall    StorageKind = "nandInstall"
	KindSaves          StorageKind = "saves"
	KindAlbum          StorageKind = "album"
	KindGamecard       StorageKind = "gamecard"
	KindCustom         StorageKind = "custom"
	KindUnknownStorage StorageKind = "unknown"
)

// StorageCapabilities tells the UI what it may offer for a storage.
type StorageCapabilities struct {
	Browse        bool `json:"browse"`
	Read          bool `json:"read"`
	Write         bool `json:"write"`
	Delete        bool `json:"delete"`
	Rename        bool `json:"rename"`
	MakeDirectory bool `json:"makeDirectory"`
	InstallTarget bool `json:"installTarget"`
}

// Storage is one MTP storage, enriched with the DBI-aware classification.
//
// The Sid/Info shape is dictated by the existing Swift client and must be
// preserved exactly; Kind, Capabilities, Virtual and the rest are additive.
type Storage struct {
	Sid  uint32          `json:"Sid"`
	Info mtp.StorageInfo `json:"Info"`

	Kind         StorageKind         `json:"kind"`
	Capabilities StorageCapabilities `json:"capabilities"`
	DisplayName  string              `json:"displayName"`
	Description  string              `json:"description,omitempty"`
	Virtual      bool                `json:"virtual"`
	SizeReliable bool                `json:"sizeReliable"`
	Order        int                 `json:"order"`
}

// dbiStorageRule maps DBI's storage descriptions onto our model.
//
// DBI names its storages with fixed English strings, so matching on the
// description is reliable for DBI itself. Anything unrecognised (including
// user-defined custom storages, which DBI supports) falls through to the
// AccessCapability-based classification, so we never hard-fail on an unknown
// name.
type dbiStorageRule struct {
	match []string
	kind  StorageKind
	order int
}

var dbiStorageRules = []dbiStorageRule{
	{[]string{"sd card install", "sdcard install", "install to sd"}, KindSDInstall, 20},
	{[]string{"nand install", "install to nand"}, KindNandInstall, 21},
	{[]string{"installed games", "installed applications"}, KindInstalledGames, 30},
	{[]string{"nand user", "nand: user"}, KindNandUser, 40},
	{[]string{"nand system", "nand: system"}, KindNandSystem, 41},
	{[]string{"gamecard", "game card"}, KindGamecard, 50},
	{[]string{"album", "screenshots"}, KindAlbum, 15},
	{[]string{"saves", "savedata", "save data"}, KindSaves, 10},
	{[]string{"sd card", "sdcard", "sd:"}, KindSDCard, 0},
}

// classifyStorage determines the kind and capabilities of a storage.
func classifyStorage(sid uint32, info *mtp.StorageInfo, profile DeviceProfile, caps *Capabilities) Storage {
	desc := strings.TrimSpace(info.StorageDescription)
	if desc == "" {
		desc = strings.TrimSpace(info.VolumeLabel)
	}

	s := Storage{
		Sid:          sid,
		Info:         *info,
		Kind:         KindUnknownStorage,
		DisplayName:  desc,
		SizeReliable: true,
		Order:        100,
	}
	if s.DisplayName == "" {
		s.DisplayName = "Storage"
	}

	isSwitch := profile == ProfileSwitchDBI || profile == ProfileSwitchHOS
	if isSwitch {
		lower := strings.ToLower(desc)
		for _, rule := range dbiStorageRules {
			if matchAny(lower, rule.match) {
				s.Kind = rule.kind
				s.Order = rule.order
				break
			}
		}
	}

	// A Switch storage we could not name, or any non-Switch device: fall back
	// to the storage's declared access capability.
	if s.Kind == KindUnknownStorage {
		if isSwitch {
			s.Kind = KindCustom
			s.Order = 60
		}
		s.Capabilities = capsFromAccess(info.AccessCapability, caps)
		s.DisplayName = friendlyName(s.Kind, desc)
		return s
	}

	s.Capabilities, s.Virtual, s.SizeReliable, s.Description = dbiCapabilities(s.Kind, info, caps)
	s.DisplayName = friendlyName(s.Kind, desc)
	return s
}

// dbiCapabilities encodes what each DBI storage actually permits.
//
// These rules are deliberately stricter than the device's declared
// AccessCapability. DBI's install storages, for example, advertise themselves
// as writable directories, but listing them is meaningless and writing to a
// subdirectory does not work -- they exist purely as drop targets that trigger
// an installation.
func dbiCapabilities(kind StorageKind, info *mtp.StorageInfo, caps *Capabilities) (c StorageCapabilities, virtual, sizeReliable bool, description string) {
	sizeReliable = true
	switch kind {
	case KindSDCard:
		c = StorageCapabilities{Browse: true, Read: true, Write: true,
			Delete: caps.CanDelete, Rename: caps.CanRename, MakeDirectory: true}
		description = "The Switch's microSD card."

	case KindSaves:
		c = StorageCapabilities{Browse: true, Read: true, Write: true,
			Delete: caps.CanDelete, Rename: caps.CanRename}
		description = "Game save data. Writing here overwrites the save on the console."

	case KindAlbum:
		c = StorageCapabilities{Browse: true, Read: true, Delete: caps.CanDelete}
		description = "Screenshots and video captures."

	case KindNandUser:
		c = StorageCapabilities{Browse: true, Read: true}
		description = "Internal storage (user partition), read-only."

	case KindNandSystem:
		c = StorageCapabilities{Browse: true, Read: true}
		description = "Internal storage (system partition), read-only. Do not modify."

	case KindInstalledGames:
		// Virtual: DBI synthesises an NSP on the fly as it is read. Nothing
		// here is a real file, so mutation is meaningless and the advertised
		// size cannot be trusted.
		c = StorageCapabilities{Browse: true, Read: true}
		virtual = true
		sizeReliable = false
		description = "Installed titles, presented as NSP files generated on demand. Read-only."

	case KindGamecard:
		c = StorageCapabilities{Browse: true, Read: true}
		virtual = true
		sizeReliable = false
		description = "The inserted game card, presented as a dump. Read-only."

	case KindSDInstall:
		// Write-only trigger. Browse is false so the UI renders it as a drop
		// target rather than a folder.
		c = StorageCapabilities{Write: true, InstallTarget: true}
		description = "Drop an NSP, NSZ, XCI or XCZ here to install it to the SD card."

	case KindNandInstall:
		c = StorageCapabilities{Write: true, InstallTarget: true}
		description = "Drop an NSP, NSZ, XCI or XCZ here to install it to internal storage."

	case KindCustom:
		c = capsFromAccess(info.AccessCapability, caps)
		description = "User-defined storage."

	default:
		c = capsFromAccess(info.AccessCapability, caps)
	}
	return narrowByAccess(c, info.AccessCapability, caps), virtual, sizeReliable, description
}

// narrowByAccess intersects the kind-based rules with what the device says it
// permits, so the result is never more permissive than the storage itself.
//
// The kind rules encode what DBI's storages mean, but not how the user has
// configured them: DBI can expose the NAND partitions read-only or writable,
// and offering an action the device then refuses is worse than not offering it.
// Narrowing only ever removes capabilities, so a storage the rules already
// restrict stays restricted.
//
// Install targets are exempt. Their entire purpose is to accept a write, and
// the capability is derived from the storage's identity rather than its
// declared access mode.
func narrowByAccess(c StorageCapabilities, access uint16, caps *Capabilities) StorageCapabilities {
	if c.InstallTarget {
		return c
	}
	switch access {
	case mtp.AC_ReadOnly:
		c.Write = false
		c.Delete = false
		c.Rename = false
		c.MakeDirectory = false
	case mtp.AC_ReadOnly_with_Object_Deletion:
		c.Write = false
		c.Rename = false
		c.MakeDirectory = false
		c.Delete = c.Delete && caps.CanDelete
	}
	return c
}

// capsFromAccess derives capabilities from the MTP AccessCapability field,
// which is all we have for a generic device.
func capsFromAccess(access uint16, caps *Capabilities) StorageCapabilities {
	switch access {
	case mtp.AC_ReadOnly:
		return StorageCapabilities{Browse: true, Read: true}
	case mtp.AC_ReadOnly_with_Object_Deletion:
		return StorageCapabilities{Browse: true, Read: true, Delete: caps.CanDelete}
	default: // AC_ReadWrite
		return StorageCapabilities{
			Browse: true, Read: true, Write: true,
			Delete: caps.CanDelete, Rename: caps.CanRename, MakeDirectory: true,
		}
	}
}

func friendlyName(kind StorageKind, raw string) string {
	switch kind {
	case KindSDCard:
		return "SD Card"
	case KindSaves:
		return "Saves"
	case KindAlbum:
		return "Album"
	case KindNandUser:
		return "Internal (User)"
	case KindNandSystem:
		return "Internal (System)"
	case KindInstalledGames:
		return "Installed Games"
	case KindGamecard:
		return "Game Card"
	case KindSDInstall:
		return "Install to SD Card"
	case KindNandInstall:
		return "Install to Internal"
	}
	if raw == "" {
		return "Storage"
	}
	return raw
}

func matchAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

// InstallableExtensions are the file types DBI's install storages accept.
var InstallableExtensions = []string{".nsp", ".nsz", ".xci", ".xcz"}

// IsInstallable reports whether a filename is something an install storage
// will accept.
func IsInstallable(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range InstallableExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
