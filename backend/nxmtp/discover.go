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
		// Both DBI and Horizon OS answer on this product ID. DBI identifies
		// itself in the MTP DeviceInfo, which we only see once connected, so
		// the refinement happens later in refineProfile.
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

// refineProfile upgrades a Switch profile to switchDBI once we can see the MTP
// DeviceInfo. DBI reports itself in the Manufacturer/Model strings, whereas
// stock Horizon OS reports Nintendo.
func refineProfile(p DeviceProfile, info *mtp.DeviceInfo) DeviceProfile {
	if p != ProfileSwitchHOS && p != ProfileSwitchDBI {
		return p
	}
	hay := strings.ToLower(info.Manufacturer + " " + info.Model + " " + info.DeviceVersion + " " + info.MTPExtension)
	if strings.Contains(hay, "dbi") || strings.Contains(hay, "duckbill") {
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

// describeCandidate opens a candidate briefly to read its identity.
func describeCandidate(dev *mtp.Device) (DeviceRef, bool) {
	if err := dev.Open(); err != nil {
		// Most commonly this is another macOS process holding the interface
		// (Image Capture Extension, PTPCamera, Android File Transfer Agent).
		// We cannot read the identity, so the device cannot be listed; the
		// diagnostics path reports the occupying process instead.
		return DeviceRef{}, false
	}
	defer dev.Close()

	usbInfo, err := dev.GetUsbInfo()
	if err != nil {
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

	// Refine using the MTP DeviceInfo, which distinguishes DBI from stock HOS.
	// A session is required for GetDeviceInfo on some responders, but DBI
	// answers it without one, so we try the cheap path and tolerate failure.
	var di mtp.DeviceInfo
	if err := dev.GetDeviceInfo(&di); err == nil {
		ref.Profile = refineProfile(ref.Profile, &di)
		if ref.Manufacturer == "" {
			ref.Manufacturer = strings.TrimSpace(di.Manufacturer)
		}
		if ref.Model == "" {
			ref.Model = strings.TrimSpace(di.Model)
		}
		if ref.SerialNumber == "" {
			ref.SerialNumber = strings.TrimSpace(di.SerialNumber)
		}
	}

	ref.DisplayName = displayNameFor(ref.Profile, ref.Manufacturer, ref.Model)
	return ref, true
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
