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

package fake

import (
	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// New builds a fake DBI device populated with a representative filesystem.
func New(opts Options) *Device {
	d := &Device{
		opts:       opts,
		nodes:      make(map[uint32]*node),
		storageInf: make(map[uint32]*mtp.StorageInfo),
		nextHandle: 0x1000,
		timeoutMs:  30000,
		Calls:      make(map[string]int),
	}
	d.seedStorages()
	d.seedFiles()
	return d
}

func (d *Device) seedStorages() {
	const gib = uint64(1) << 30

	// Descriptions match DBI's exactly: nxmtp classifies storages by
	// description first, so a typo here would silently defeat the test.
	add := func(sid uint32, desc string, access uint16, max, free uint64) {
		d.storages = append(d.storages, sid)
		d.storageInf[sid] = &mtp.StorageInfo{
			StorageType:        mtp.ST_RemovableRAM,
			FilesystemType:     mtp.FST_GenericHierarchical,
			AccessCapability:   access,
			MaxCapability:      max,
			FreeSpaceInBytes:   free,
			StorageDescription: desc,
			VolumeLabel:        desc,
		}
	}

	add(SidSDCard, "SD Card", mtp.AC_ReadWrite, 400*gib, 220*gib)
	add(SidNandUser, "Nand USER", mtp.AC_ReadOnly, 26*gib, 3*gib)
	add(SidNandSystem, "Nand SYSTEM", mtp.AC_ReadOnly, 2*gib, 512*gib/1024)
	// Virtual storages report no capacity at all, which is why the UI must not
	// divide by MaxCapability without guarding.
	add(SidInstalledGames, "Installed games", mtp.AC_ReadOnly, 0, 0)
	add(SidSDInstall, "SD Card install", mtp.AC_ReadWrite, 0, 0)
	add(SidNandInstall, "NAND install", mtp.AC_ReadWrite, 0, 0)
	add(SidSaves, "Saves", mtp.AC_ReadWrite, 0, 0)
	add(SidAlbum, "Album", mtp.AC_ReadOnly, 0, 0)
	add(SidGamecard, "Gamecard", mtp.AC_ReadOnly, 32*gib, 0)
}

func (d *Device) seedFiles() {
	sd := d.mkdirRoot(SidSDCard, "switch")
	d.addFile(SidSDCard, sd, "prod.keys", []byte("dummy-keys"))
	d.addFile(SidSDCard, sd, "hbmenu.nro", make([]byte, 4096))

	games := d.mkdirRoot(SidSDCard, "games")
	d.addFile(SidSDCard, games, "small.nsp", []byte("nsp-contents"))

	// A filename with an astral-plane character: MTP encodes strings as
	// UTF-16, so this exercises surrogate-pair decoding.
	d.addFile(SidSDCard, games, "emoji-\U0001F600-title.nsp", []byte("x"))

	d.addFile(SidSDCard, 0, "readme.txt", []byte("hello from the fake switch"))

	// Installed games are generated NSP dumps: read-only, virtual, and larger
	// than 4 GiB, so CompressedSize overflows and the real size is only
	// available through GetObjectPropValue.
	big := d.addFile(SidInstalledGames, 0, "0100000000010000 [v0].nsp", nil)
	big.virtual = true
	big.declaredSize = 6 * (1 << 30)

	small := d.addFile(SidInstalledGames, 0, "0100000000020000 [v0].nsp", []byte("tiny-dump"))
	small.virtual = true

	saveDir := d.mkdirRoot(SidSaves, "0100000000010000")
	d.addFile(SidSaves, saveDir, "savedata.bin", []byte("save-bytes"))

	d.addFile(SidAlbum, 0, "2024010112000000.jpg", []byte("jpeg"))
}

func (d *Device) mkdirRoot(storage uint32, name string) uint32 {
	n := d.addNode(storage, 0, name, true, nil)
	return n.handle
}

func (d *Device) addFile(storage, parent uint32, name string, data []byte) *node {
	return d.addNode(storage, parent, name, false, data)
}

func (d *Device) addNode(storage, parent uint32, name string, isDir bool, data []byte) *node {
	n := &node{
		handle:   d.nextHandle,
		parent:   parent,
		storage:  storage,
		name:     name,
		isDir:    isDir,
		data:     data,
		modified: seedTime,
	}
	d.nextHandle++
	d.nodes[n.handle] = n
	return n
}
