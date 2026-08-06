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
	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// Nintendo USB identifiers.
//
// DBI's MTP responder reuses the same product ID as Horizon OS's own MTP
// implementation, so a device on 0x201D may be either.
//
// PID 0x3000 is NOT DBI-specific: it is libnx's generic homebrew usbComms
// product ID, presented by any homebrew that opens a vendor-specific USB
// interface. DBI's "DBIbackend" mode, Awoo-Installer's "Install from USB" and
// GoldLeaf's Quark protocol all appear here, each speaking a different,
// non-MTP protocol. We detect it so we can give real advice instead of "no
// device found", but we must not claim to know which of them is running.
const (
	VendorNintendo = 0x057E

	ProductSwitchMTP     = 0x201D // Switch (HOS or DBI) MTP responder
	ProductSwitch2MTP    = 0x2061 // Switch 2 MTP responder
	ProductHomebrewUSB   = 0x3000 // libnx usbComms: DBIbackend/Awoo/GoldLeaf -- NOT MTP
	ProductSwitchGeneric = 0x2000 // Switch in other USB modes
)

// DeviceProfile describes what we believe we are talking to. It drives timeout
// tuning, storage classification and the advice shown in the UI.
type DeviceProfile string

const (
	ProfileGeneric     DeviceProfile = "generic"
	ProfileSwitchDBI   DeviceProfile = "switchDBI"
	ProfileSwitchHOS   DeviceProfile = "switchHOS"
	ProfileHomebrewUSB DeviceProfile = "homebrewUSB"
)

// Capabilities records which optional MTP operations the device advertised in
// its DeviceInfo, plus any that we discovered at runtime to be broken.
//
// This type is the single most important robustness mechanism in nxmtp. The
// previous generation of this stack (go-mtpx) called GetObjectPropValue on
// every directory entry unconditionally; on a device that does not implement
// it, an entire directory listing fails. Here, every optional call is gated on
// Capabilities, and a runtime OperationNotSupported permanently demotes the
// capability so we stop asking.
type Capabilities struct {
	GetObjectPropList      bool `json:"getObjectPropList"`
	GetObjectPropValue     bool `json:"getObjectPropValue"`
	SetObjectPropValue     bool `json:"setObjectPropValue"`
	GetObjectPropsSupport  bool `json:"getObjectPropsSupported"`
	GetPartialObject       bool `json:"getPartialObject"`
	MoveObject             bool `json:"moveObject"`
	CopyObject             bool `json:"copyObject"`
	GetNumObjects          bool `json:"getNumObjects"`
	DeleteObject           bool `json:"deleteObject"`
	SendObject             bool `json:"sendObject"`
	AndroidExtension       bool `json:"androidExtension"`
	AndroidPartialTransfer bool `json:"androidPartialTransfer"`

	// Derived, user-facing flags. The Swift UI greys out actions based on
	// these rather than re-deriving the rules.
	CanRename bool `json:"canRename"`
	CanDelete bool `json:"canDelete"`
	CanMove   bool `json:"canMove"`
}

// capsFromDeviceInfo builds a Capabilities from the operations the device
// advertised.
func capsFromDeviceInfo(info *mtp.DeviceInfo) *Capabilities {
	ops := make(map[uint16]bool, len(info.OperationsSupported))
	for _, op := range info.OperationsSupported {
		ops[op] = true
	}

	c := &Capabilities{
		GetObjectPropList:     ops[mtp.OC_MTP_GetObjPropList],
		GetObjectPropValue:    ops[mtp.OC_MTP_GetObjectPropValue],
		SetObjectPropValue:    ops[mtp.OC_MTP_SetObjectPropValue],
		GetObjectPropsSupport: ops[mtp.OC_MTP_GetObjectPropsSupported],
		GetPartialObject:      ops[mtp.OC_GetPartialObject],
		MoveObject:            ops[mtp.OC_MoveObject],
		CopyObject:            ops[mtp.OC_CopyObject],
		GetNumObjects:         ops[mtp.OC_GetNumObjects],
		DeleteObject:          ops[mtp.OC_DeleteObject],
		SendObject:            ops[mtp.OC_SendObject],
	}

	// DBI advertises the Android extension by default (ReportAndroidExtension
	// is on out of the box). We record it for diagnostics but never call the
	// Android operations: they are not needed for anything we do, and DBI's
	// implementation of them is undocumented.
	c.AndroidExtension = hasAndroidExtension(info.MTPExtension)
	c.AndroidPartialTransfer = c.AndroidExtension &&
		ops[mtp.OC_ANDROID_GET_PARTIAL_OBJECT64] &&
		ops[mtp.OC_ANDROID_SEND_PARTIAL_OBJECT]

	c.recompute()
	return c
}

// recompute refreshes the derived user-facing flags.
func (c *Capabilities) recompute() {
	c.CanRename = c.SetObjectPropValue
	c.CanDelete = c.DeleteObject
	c.CanMove = c.MoveObject || (c.CopyObject && c.DeleteObject)
}

// demote records that an operation the device advertised turned out not to
// work, so we stop attempting it for the rest of the session. Returns true if
// this actually changed something.
func (c *Capabilities) demote(op uint16) bool {
	changed := false
	set := func(p *bool) {
		if *p {
			*p = false
			changed = true
		}
	}
	switch op {
	case mtp.OC_MTP_GetObjPropList:
		set(&c.GetObjectPropList)
	case mtp.OC_MTP_GetObjectPropValue:
		set(&c.GetObjectPropValue)
	case mtp.OC_MTP_SetObjectPropValue:
		set(&c.SetObjectPropValue)
	case mtp.OC_MTP_GetObjectPropsSupported:
		set(&c.GetObjectPropsSupport)
	case mtp.OC_GetPartialObject:
		set(&c.GetPartialObject)
	case mtp.OC_MoveObject:
		set(&c.MoveObject)
	case mtp.OC_CopyObject:
		set(&c.CopyObject)
	case mtp.OC_GetNumObjects:
		set(&c.GetNumObjects)
	}
	if changed {
		c.recompute()
	}
	return changed
}

func hasAndroidExtension(ext string) bool {
	return len(ext) > 0 && containsToken(ext, "android.com")
}

// containsToken reports whether the MTP extension string (a semicolon or
// space separated list) mentions the given vendor token.
func containsToken(s, token string) bool {
	n := len(token)
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == token {
			return true
		}
	}
	return false
}
