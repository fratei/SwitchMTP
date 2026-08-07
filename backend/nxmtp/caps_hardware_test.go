package nxmtp

import (
	"testing"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// dbiOperationsSupported is the exact OperationsSupported list reported by DBI's
// MTP responder on a Nintendo Switch (HOS 22.5.0, serial-verified capture).
//
// The public documentation never confirmed which optional operations DBI
// implements, so the capability layer was written to probe rather than assume.
// This pins the answer we measured, so a regression in the parsing shows up as
// a test failure rather than as a broken listing on real hardware.
var dbiOperationsSupported = []uint16{
	0x1001, 0x1002, 0x1003, 0x1004, 0x1005, 0x1007, 0x1008, 0x1009,
	0x100b, 0x100c, 0x100d, 0x1014, 0x1015, 0x1016, 0x1019, 0x101b,
	0x95c1, 0x95c2, 0x95c3, 0x95c4, 0x95c5,
	0x9801, 0x9802, 0x9803, 0x9804, 0x9805, 0x9808,
}

func TestCapabilitiesFromRealDBIDevice(t *testing.T) {
	c := capsFromDeviceInfo(&mtp.DeviceInfo{OperationsSupported: dbiOperationsSupported})

	// Confirmed present. GetObjectPropList in particular decides whether
	// listing takes one round trip or one per file.
	for name, got := range map[string]bool{
		"GetObjectPropList":  c.GetObjectPropList,
		"GetObjectPropValue": c.GetObjectPropValue,
		"SetObjectPropValue": c.SetObjectPropValue,
		"GetPartialObject":   c.GetPartialObject,
		"MoveObject":         c.MoveObject,
		"DeleteObject":       c.DeleteObject,
		"SendObject":         c.SendObject,
	} {
		if !got {
			t.Errorf("%s: want supported, got unsupported", name)
		}
	}

	// Confirmed absent. Calling these unconditionally is what breaks listing
	// against DBI, so the negative case matters as much as the positive one.
	if c.CopyObject {
		t.Error("CopyObject: DBI does not implement 0x101a, want unsupported")
	}
	if c.GetNumObjects {
		t.Error("GetNumObjects: DBI does not implement 0x1006, want unsupported")
	}

	// Rename needs SetObjectPropValue; move can be native here.
	if !c.CanRename {
		t.Error("CanRename: want true, DBI advertises SetObjectPropValue")
	}
	if !c.CanMove {
		t.Error("CanMove: want true, DBI advertises MoveObject")
	}
}
