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

// Package fake implements an in-process MTP responder that behaves like DBI on
// a Nintendo Switch, so the nxmtp layer can be tested without hardware.
//
// It deliberately reproduces DBI's awkward parts rather than an idealised MTP
// device, because those are what the code under test exists to handle:
//
//   - nine storages, several of them read-only, write-only, or virtual
//   - optional property operations that may be unadvertised, or advertised and
//     then refused at runtime
//   - files larger than 4 GiB whose size overflows ObjectInfo.CompressedSize
//   - installs that report success but have no completion event
//
// Each of those behaviours is switchable so a test can pin down exactly one.
package fake

import (
	"sync"
	"time"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// Storage ids as DBI assigns them. The values are arbitrary but stable, and
// distinct enough that a test failure names the storage unambiguously.
const (
	SidSDCard         uint32 = 0x00010001
	SidNandUser       uint32 = 0x00020001
	SidNandSystem     uint32 = 0x00030001
	SidInstalledGames uint32 = 0x00040001
	SidSDInstall      uint32 = 0x00050001
	SidNandInstall    uint32 = 0x00060001
	SidSaves          uint32 = 0x00070001
	SidAlbum          uint32 = 0x00080001
	SidGamecard       uint32 = 0x00090001
)

// Options selects which DBI quirks the fake exhibits.
//
// The zero value is a healthy, fully featured device: every optional operation
// is advertised and works. Tests turn individual failures on.
type Options struct {
	// NoPropList removes GetObjectPropList from the advertised operation set,
	// forcing the GetObjectHandles + GetObjectInfo fallback.
	NoPropList bool
	// NoPropValue removes GetObjectPropValue, which is what makes the true size
	// of a >4 GiB file unknowable.
	NoPropValue bool
	// NoSetPropValue removes SetObjectPropValue, which disables rename.
	NoSetPropValue bool

	// SecretlyUnsupported advertises the property operations but fails them at
	// runtime. This is the nastier variant, and the reason the client demotes
	// capabilities on refusal rather than trusting GetDeviceInfo.
	SecretlyUnsupported bool

	// FailSendObjectAfter aborts a SendObject once this many bytes have been
	// consumed, emulating "media write speed is too low" in applet mode.
	// Zero means never.
	FailSendObjectAfter int64

	// SlowWrite delays each chunk during SendObject.
	SlowWrite time.Duration

	// Disconnected makes every operation fail as if the cable were pulled.
	Disconnected bool
}

type node struct {
	handle   uint32
	parent   uint32
	storage  uint32
	name     string
	isDir    bool
	data     []byte
	modified time.Time

	// declaredSize overrides len(data) in ObjectInfo, so a test can present a
	// >4 GiB file without allocating one.
	declaredSize int64
	// virtual marks a generated object, as in DBI's "Installed games" NSP dumps.
	virtual bool
}

// Device is the fake responder. It satisfies nxmtp.Transport.
type Device struct {
	mu   sync.Mutex
	opts Options

	nodes      map[uint32]*node
	storages   []uint32
	storageInf map[uint32]*mtp.StorageInfo
	nextHandle uint32

	sessionOpen bool
	pending     *node
	closed      bool
	timeoutMs   int

	// Installed records objects sent to an install storage, so a test can
	// assert the install flow reached the right place.
	Installed []string
	// Calls counts operations by name for assertions about, for example, how
	// many round trips a listing took.
	Calls map[string]int
}
