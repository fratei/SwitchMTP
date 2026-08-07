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
	"fmt"
	"strconv"
	"strings"

	"github.com/ganeshrvel/go-mtpfs/mtp"
	"github.com/ganeshrvel/usb"
)

// Timeouts. The MTP engine defaults to a value tuned for phones; a Switch
// writing to a slow SD card, or DBI generating a virtual NSP dump on the fly,
// can block for far longer than that before the first bulk packet arrives.
const (
	DefaultTimeoutMs = 30000
	SwitchTimeoutMs  = 120000
)

// DeviceRef identifies a device without holding it open.
type DeviceRef struct {
	VendorID     uint16 `json:"vendorId"`
	ProductID    uint16 `json:"productId"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	SerialNumber string `json:"serialNumber"`

	// SwitchMTP additions, used by the UI to explain what it is looking at.
	Profile     DeviceProfile `json:"profile"`
	Usable      bool          `json:"usable"`
	Advice      string        `json:"advice,omitempty"`
	DisplayName string        `json:"displayName"`
}

// ID returns the canonical device identifier used across the FFI boundary:
// "<vendorId>|<productId>|<serialNumber>" with decimal integers. The Swift app
// parses exactly this format, so it must not change.
func (r DeviceRef) ID() string {
	return fmt.Sprintf("%d|%d|%s", r.VendorID, r.ProductID, r.SerialNumber)
}

// ParseDeviceID splits a canonical device identifier.
func ParseDeviceID(id string) (vendorID, productID uint16, serial string, err error) {
	parts := strings.SplitN(id, "|", 3)
	if len(parts) != 3 {
		return 0, 0, "", errf(KindInvalidInput, "parseDeviceId", "malformed device id %q", id)
	}
	v, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return 0, 0, "", errf(KindInvalidInput, "parseDeviceId", "bad vendor id in %q", id)
	}
	p, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return 0, 0, "", errf(KindInvalidInput, "parseDeviceId", "bad product id in %q", id)
	}
	return uint16(v), uint16(p), parts[2], nil
}

// profileFor classifies a USB vendor/product pair.
func profileFor(vendorID, productID uint16, manufacturer, model string) (DeviceProfile, bool, string) {
	if vendorID != VendorNintendo {
		return ProfileGeneric, true, ""
	}
	switch productID {
	case ProductSwitchMTP, ProductSwitch2MTP:
		// Both DBI and Horizon OS answer on this product ID.
		//
		// Hardware testing showed DBI reports Manufacturer "Nintendo" and
		// Model "Switch" in its *MTP* DeviceInfo -- indistinguishable from
		// stock Horizon OS. It identifies itself only in the USB descriptors
		// (product string "DBI", interface string "DBI MTP"), which is why we
		// classify from those here rather than waiting for DeviceInfo.
		if looksLikeDBI(manufacturer, model) {
			return ProfileSwitchDBI, true, ""
		}
		return ProfileSwitchHOS, true, ""
	case ProductHomebrewUSB:
		// 0x3000 is shared by every libnx homebrew USB mode, so we cannot say
		// which one is running. Lead with the fix that works regardless.
		return ProfileHomebrewUSB, false, "This Switch is in a homebrew USB mode, which is not MTP. " +
			"SwitchMTP needs DBI's MTP responder: on the Switch, open DBI, press X, and choose \"Run MTP responder\". " +
			"If you are in DBI's \"Install title from DBIbackend\" screen, back out of it first. " +
			"Awoo-Installer and GoldLeaf also appear this way; SwitchMTP does not speak their protocols, so exit them and start DBI instead."
	default:
		return ProfileGeneric, true, ""
	}
}

// looksLikeDBI reports whether USB identity strings came from DBI's MTP
// responder. DBI advertises itself in the USB product string ("DBI") and the
// interface string ("DBI MTP"); this is the only place it names itself, so it
// is the only reliable discriminator against stock Horizon OS.
func looksLikeDBI(fields ...string) bool {
	hay := strings.ToLower(strings.Join(fields, " "))
	return strings.Contains(hay, "dbi") || strings.Contains(hay, "duckbill")
}

// refineProfile is a second chance at DBI detection for responders whose USB
// strings were unreadable. It only ever upgrades: a device already identified
// as DBI from its USB descriptors is never demoted, because DBI's MTP
// DeviceInfo deliberately mimics stock Horizon OS ("Nintendo"/"Switch").
func refineProfile(p DeviceProfile, info *mtp.DeviceInfo) DeviceProfile {
	if p == ProfileSwitchDBI {
		return p
	}
	if p != ProfileSwitchHOS {
		return p
	}
	if looksLikeDBI(info.Manufacturer, info.Model, info.DeviceVersion, info.MTPExtension) {
		return ProfileSwitchDBI
	}
	return p
}

// timeoutFor returns the USB timeout appropriate to a device profile.
func timeoutFor(p DeviceProfile) int {
	switch p {
	case ProfileSwitchDBI, ProfileSwitchHOS:
		return SwitchTimeoutMs
	default:
		return DefaultTimeoutMs
	}
}

func displayNameFor(p DeviceProfile, manufacturer, model string) string {
	switch p {
	case ProfileSwitchDBI:
		return "Nintendo Switch (DBI)"
	case ProfileSwitchHOS:
		return "Nintendo Switch"
	case ProfileHomebrewUSB:
		return "Nintendo Switch (homebrew USB mode)"
	}
	name := strings.TrimSpace(manufacturer + " " + model)
	if name == "" {
		return "MTP device"
	}
	return name
}

// FindDevices enumerates attached MTP-capable devices without keeping any of
// them open.
//
// Each candidate is opened just long enough to read its USB identity, then
// closed again. MTP allows only one session at a time, so holding devices open
// during enumeration would make them unavailable to the caller (and to other
// applications).
//
// Nintendo devices found in a homebrew USB mode are reported with Usable=false and
// an Advice string rather than being silently dropped -- telling the user they
// picked the wrong DBI menu entry is far more useful than an empty list.
func FindDevices() ([]DeviceRef, error) {
	ctx := usb.NewContext()
	defer ctx.Exit()

	cands, err := mtp.FindDevices(ctx)
	if err != nil {
		return nil, wrapErr(KindUnknown, "findDevices", err)
	}

	refs := make([]DeviceRef, 0, len(cands))
	for _, dev := range cands {
		ref, ok := describeCandidate(dev)
		if ok {
			refs = append(refs, ref)
		}
		dev.Done()
	}

	// A Switch sitting in a homebrew USB mode does not present an MTP interface at
	// all, so the loop above will not have seen it. Scan the raw USB device
	// list for it separately.
	refs = append(refs, findNonMTPNintendo(ctx, refs)...)

	return refs, nil
}

// describeCandidate reads a candidate's identity without disturbing it.
func describeCandidate(dev *mtp.Device) (DeviceRef, bool) {
	accept, needsStringCheck := looksLikeMTPInterface(dev)
	if !accept {
		// A look-alike: the right endpoint layout but the wrong class. Not
		// opened, so it is never claimed and never reset.
		return DeviceRef{}, false
	}

	usbInfo, err := readIdentity(dev, needsStringCheck)
	if err != nil {
		// Most commonly this is another macOS process holding the interface
		// (Image Capture Extension, ptpcamerad, Android File Transfer Agent).
		// We cannot read the identity, so the device cannot be listed; the
		// diagnostics path reports the occupying process instead.
		return DeviceRef{}, false
	}

	ref := DeviceRef{
		VendorID:     usbInfo.IdVendor,
		ProductID:    usbInfo.IdProduct,
		Manufacturer: strings.TrimSpace(usbInfo.Manufacturer),
		Model:        strings.TrimSpace(usbInfo.Product),
		SerialNumber: strings.TrimSpace(usbInfo.SerialNumber),
	}
	ref.Profile, ref.Usable, ref.Advice = profileFor(ref.VendorID, ref.ProductID, ref.Manufacturer, ref.Model)

	// The MTP DeviceInfo is deliberately NOT consulted here. Reading it needs
	// a claimed interface, and claiming during a scan is what caused the
	// reset storm described on mtp.OpenIdentity. It buys nothing either:
	// DBI reports the same Manufacturer/Model as stock horizon ("Nintendo" /
	// "Switch"), so the USB product string is the only discriminator, and it
	// has already been applied above. Sessions refine the profile properly --
	// see refineProfile in client.go.

	ref.DisplayName = displayNameFor(ref.Profile, ref.Manufacturer, ref.Model)
	return ref, true
}

// USB interface classes relevant to MTP identification.
const (
	usbClassStillImage     = 6   // PIMA 15740 -- the class PTP and MTP live in
	usbClassVendorSpecific = 255 // used by MTP-only devices that omit PTP
	usbSubClassStillImage  = 1
	usbProtocolPTP         = 1
)

// looksLikeMTPInterface decides whether a candidate is worth opening.
//
// mtp.FindDevices matches any interface carrying exactly one bulk-in, one
// bulk-out and one interrupt-in endpoint. That is the PTP endpoint layout, but
// it is not unique to PTP: USB Ethernet adapters use the same shape, so a
// Realtek RTL8156 dock happily appears in the device list next to the Switch.
//
// Two forms are accepted, mirroring how libmtp identifies devices:
//
//   - class 6 / subclass 1 / protocol 1 -- canonical still-image PTP. DBI's
//     responder reports exactly this.
//   - class 255 (vendor specific) whose interface string contains "MTP" --
//     how MTP-only devices, chiefly Android handsets, advertise themselves.
//
// The second form needs a string descriptor, so a vendor-specific interface
// that declares no string is rejected outright. That is decided from cached
// descriptor data alone: the offending Ethernet adapter reports string index 0
// and is dropped without ever being opened.
//
// needsStringCheck reports that acceptance is provisional and must be
// confirmed by confirmMTPInterface once a handle is available.
func looksLikeMTPInterface(dev *mtp.Device) (accept, needsStringCheck bool) {
	class, subClass, protocol, stringIndex := dev.InterfaceInfo()
	return classifyInterface(class, subClass, protocol, stringIndex)
}

// classifyInterface holds the decision logic for looksLikeMTPInterface, split
// out so it can be tested without a USB device.
func classifyInterface(class, subClass, protocol, stringIndex byte) (accept, needsStringCheck bool) {
	switch class {
	case usbClassStillImage:
		// Subclass/protocol are checked loosely: every real device uses
		// 1/1, but rejecting on them would be a needless failure mode for
		// a device that got them slightly wrong yet still speaks MTP.
		_ = subClass
		_ = protocol
		return true, false
	case usbClassVendorSpecific:
		if stringIndex == 0 {
			return false, false
		}
		return true, true
	default:
		return false, false
	}
}

// confirmMTPInterface completes the check begun by looksLikeMTPInterface for
// vendor-specific interfaces. The device must already be open.
func confirmMTPInterface(dev *mtp.Device) bool {
	s, err := dev.InterfaceString()
	if err != nil {
		// The descriptor promised a string and the device would not give it
		// up. Treat that as "not MTP" rather than guessing.
		return false
	}
	return strings.Contains(strings.ToUpper(s), "MTP")
}

// readIdentity reads a candidate's USB string descriptors.
//
// The fast path opens the handle without claiming the MTP interface, so
// discovery never resets the port. Should that fail -- an old libusb, or a
// macOS release that refuses to hand out a device user client -- it falls back
// to the full claiming open, which is what the code did unconditionally
// before.
//
// confirmInterface asks for the vendor-specific interface-string check to be
// run while the handle is open; it is only set for candidates whose class did
// not already prove they are still-image devices.
func readIdentity(dev *mtp.Device, confirmInterface bool) (*mtp.UsbDeviceInfo, error) {
	if err := dev.OpenIdentity(); err == nil {
		if confirmInterface && !confirmMTPInterface(dev) {
			dev.Close()
			return nil, errf(KindNotFound, "readIdentity", "not an MTP interface")
		}
		info, infoErr := dev.GetUsbInfo()
		dev.Close()
		if infoErr == nil {
			return info, nil
		}
	}

	if err := dev.Open(); err != nil {
		return nil, err
	}
	defer dev.Close()
	if confirmInterface && !confirmMTPInterface(dev) {
		return nil, errf(KindNotFound, "readIdentity", "not an MTP interface")
	}
	return dev.GetUsbInfo()
}

// findNonMTPNintendo looks for Nintendo devices that expose no MTP interface,
// which in practice means DBI running in its custom backend mode.
func findNonMTPNintendo(ctx *usb.Context, existing []DeviceRef) []DeviceRef {
	list, err := ctx.GetDeviceList()
	if err != nil {
		return nil
	}
	defer list.Done()

	seen := make(map[uint16]bool, len(existing))
	for _, r := range existing {
		if r.VendorID == VendorNintendo {
			seen[r.ProductID] = true
		}
	}

	var out []DeviceRef
	for _, d := range list {
		desc, err := d.GetDeviceDescriptor()
		if err != nil || desc.IdVendor != VendorNintendo {
			continue
		}
		if seen[desc.IdProduct] {
			continue
		}
		seen[desc.IdProduct] = true

		profile, usable, advice := profileFor(desc.IdVendor, desc.IdProduct, "Nintendo", "")
		if profile == ProfileGeneric {
			// A Nintendo device in some mode we have no advice about; listing
			// it would only be confusing.
			continue
		}
		out = append(out, DeviceRef{
			VendorID:    desc.IdVendor,
			ProductID:   desc.IdProduct,
			Profile:     profile,
			Usable:      usable,
			Advice:      advice,
			DisplayName: displayNameFor(profile, "Nintendo", ""),
		})
	}
	return out
}

// openByID finds and opens the device matching a canonical device id.
func openByID(id string) (*mtp.Device, *usb.Context, DeviceRef, error) {
	wantVendor, wantProduct, wantSerial, err := ParseDeviceID(id)
	if err != nil {
		return nil, nil, DeviceRef{}, err
	}

	ctx := usb.NewContext()
	cands, err := mtp.FindDevices(ctx)
	if err != nil {
		ctx.Exit()
		return nil, nil, DeviceRef{}, wrapErr(KindUnknown, "open", err)
	}

	var match *mtp.Device
	var ref DeviceRef
	var openErr error

	for _, dev := range cands {
		if match != nil {
			dev.Done()
			continue
		}
		// Filter on the cached descriptor first. Opening a candidate claims
		// its interface and may reset the port, so a device that cannot
		// possibly be the one we were asked for must never be opened.
		if v, p := dev.UsbIDs(); v != wantVendor || p != wantProduct {
			dev.Done()
			continue
		}
		// Same look-alike guard as discovery: an interface with the PTP
		// endpoint layout but the wrong class is not an MTP device, and
		// opening one would claim and reset it for nothing.
		if accept, _ := looksLikeMTPInterface(dev); !accept {
			dev.Done()
			continue
		}
		if err := dev.Open(); err != nil {
			openErr = err
			dev.Done()
			continue
		}
		usbInfo, err := dev.GetUsbInfo()
		if err != nil {
			dev.Close()
			dev.Done()
			continue
		}
		serial := strings.TrimSpace(usbInfo.SerialNumber)
		if usbInfo.IdVendor != wantVendor || usbInfo.IdProduct != wantProduct {
			dev.Close()
			dev.Done()
			continue
		}
		// An empty serial on both sides still counts as a match: some
		// responders report no serial number at all.
		if wantSerial != "" && serial != "" && serial != wantSerial {
			dev.Close()
			dev.Done()
			continue
		}
		match = dev
		ref = DeviceRef{
			VendorID:     usbInfo.IdVendor,
			ProductID:    usbInfo.IdProduct,
			Manufacturer: strings.TrimSpace(usbInfo.Manufacturer),
			Model:        strings.TrimSpace(usbInfo.Product),
			SerialNumber: serial,
		}
	}

	if match == nil {
		ctx.Exit()
		if openErr != nil {
			return nil, nil, DeviceRef{}, &Error{
				Kind: KindDeviceBusy,
				Op:   "open",
				Msg:  "the device is present but could not be claimed",
				Hint: occupiedHint(),
				Err:  openErr,
			}
		}
		return nil, nil, DeviceRef{}, newErr(KindNoDevice, "open", "device not found; check the USB cable and that DBI's MTP responder is running")
	}

	ref.Profile, ref.Usable, ref.Advice = profileFor(ref.VendorID, ref.ProductID, ref.Manufacturer, ref.Model)
	return match, ctx, ref, nil
}

func occupiedHint() string {
	return "Another macOS process is holding the device. Quit Image Capture, " +
		"Android File Transfer Agent, or any camera/photo app, then unplug and " +
		"reconnect the Switch. Run the built-in diagnostics to see which process is responsible."
}
