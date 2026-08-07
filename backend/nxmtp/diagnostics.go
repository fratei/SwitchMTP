// SwitchMTP — an MTP client for Nintendo Switch running DBI.
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
	"os"
	"runtime"

	"github.com/ganeshrvel/usb"
)

// This file holds the parts of diagnostics that are the same everywhere.
// Two things genuinely differ per platform and live in diagnostics_darwin.go
// and diagnostics_other.go:
//
//   - usbClients, which asks the OS which processes hold a USB device; there
//     is no portable API for this.
//   - interfaceHolders, which narrows that to whoever holds the *interface*
//     on an attached Nintendo device.
//
// Everything else -- enumerating USB, recognising Switch hardware, deciding
// what to tell the user -- is shared, so a fix to the advice benefits every
// platform at once.

// USBClient is a process holding a USB device or interface.
type USBClient struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`
	Known   bool   `json:"known"`
	Advice  string `json:"advice,omitempty"`
	IsSelf  bool   `json:"isSelf"`
	Blocker bool   `json:"blocker"`
}

// USBDeviceSummary is one raw USB device, used to show what the host can see
// even when we cannot claim it.
type USBDeviceSummary struct {
	VendorID   uint16 `json:"vendorId"`
	ProductID  uint16 `json:"productId"`
	Class      uint8  `json:"deviceClass"`
	IsNintendo bool   `json:"isNintendo"`
	// Description is populated only for Nintendo hardware; callers that show
	// every device should fall back to the USB base class rather than render
	// an empty cell.
	Description string `json:"description"`
	// MTPCapable means the device has the endpoint layout MTP uses, matching
	// the candidate filter in the underlying MTP library. It is a structural
	// hint, not an identification: USB ethernet adapters and other hardware
	// share that layout. Do not present it to users as "this is an MTP device".
	MTPCapable bool `json:"mtpCapable"`
}

// Diagnostics is the report shown in the troubleshooting UI and attached to
// bug reports.
type Diagnostics struct {
	Platform     string             `json:"platform"`
	Arch         string             `json:"arch"`
	SelfPID      int                `json:"selfPid"`
	Devices      []USBDeviceSummary `json:"devices"`
	MTPDevices   []DeviceRef        `json:"mtpDevices"`
	USBClients   []USBClient        `json:"usbClients"`
	Blockers     []USBClient        `json:"blockers"`
	NintendoSeen bool               `json:"nintendoSeen"`
	Summary      string             `json:"summary"`
	Advice       []string           `json:"advice"`
}

// CollectDiagnostics gathers everything useful for working out why a device is
// not usable.
//
// It deliberately does not require a connected client: the whole point is to
// answer "why can I not connect".
func CollectDiagnostics() *Diagnostics {
	d := &Diagnostics{
		Platform: runtime.GOOS,
		Arch:     runtime.GOARCH,
		SelfPID:  os.Getpid(),
	}

	d.USBClients = usbClients(d.SelfPID)
	for _, c := range d.USBClients {
		if c.Blocker {
			d.Blockers = append(d.Blockers, c)
		}
	}

	d.Devices, d.NintendoSeen = enumerateUSB()

	// A machine-wide scan cannot tell whether a process is holding *our*
	// device, so anything it flags is a guess. Now that we know which Nintendo
	// devices are attached, ask the OS precisely who holds each one's interface
	// and let that override the guesswork.
	d.Blockers = append(d.Blockers, interfaceHolders(d)...)

	if refs, err := FindDevices(); err == nil {
		d.MTPDevices = refs
	}

	d.Summary, d.Advice = summarise(d)
	return d
}

// enumerateUSB lists raw USB devices, flagging Nintendo hardware and anything
// that looks MTP-capable.
func enumerateUSB() ([]USBDeviceSummary, bool) {
	ctx := usb.NewContext()
	defer ctx.Exit()

	list, err := ctx.GetDeviceList()
	if err != nil {
		return nil, false
	}
	defer list.Done()

	var out []USBDeviceSummary
	nintendo := false

	for _, dev := range list {
		desc, err := dev.GetDeviceDescriptor()
		if err != nil {
			continue
		}
		s := USBDeviceSummary{
			VendorID:   desc.IdVendor,
			ProductID:  desc.IdProduct,
			Class:      desc.DeviceClass,
			IsNintendo: desc.IdVendor == VendorNintendo,
		}
		if s.IsNintendo {
			nintendo = true
			s.Description = describeNintendo(desc.IdProduct)
		}
		s.MTPCapable = looksMTPCapable(dev)
		out = append(out, s)
	}
	return out, nintendo
}

func describeNintendo(pid uint16) string {
	switch pid {
	case ProductSwitchMTP:
		return "Nintendo Switch, MTP mode (DBI or Horizon OS)"
	case ProductSwitch2MTP:
		return "Nintendo Switch 2, MTP mode"
	case ProductHomebrewUSB:
		return "Nintendo Switch in a homebrew USB mode (DBIbackend, Awoo-Installer or GoldLeaf) -- this is NOT MTP"
	default:
		return "Nintendo device in a non-MTP USB mode"
	}
}

// looksMTPCapable reports whether a device exposes the three-endpoint
// bulk-in/bulk-out/interrupt-in layout that MTP requires.
func looksMTPCapable(dev *usb.Device) bool {
	desc, err := dev.GetDeviceDescriptor()
	if err != nil {
		return false
	}
	for i := byte(0); i < desc.NumConfigurations; i++ {
		cfg, err := dev.GetConfigDescriptor(i)
		if err != nil {
			continue
		}
		for _, iface := range cfg.Interfaces {
			for _, alt := range iface.AltSetting {
				if len(alt.EndPoints) != 3 {
					continue
				}
				var bulkIn, bulkOut, intrIn bool
				for _, ep := range alt.EndPoints {
					switch {
					case ep.Direction() == usb.ENDPOINT_IN && ep.TransferType() == usb.TRANSFER_TYPE_BULK:
						bulkIn = true
					case ep.Direction() == usb.ENDPOINT_OUT && ep.TransferType() == usb.TRANSFER_TYPE_BULK:
						bulkOut = true
					case ep.Direction() == usb.ENDPOINT_IN && ep.TransferType() == usb.TRANSFER_TYPE_INTERRUPT:
						intrIn = true
					}
				}
				if bulkIn && bulkOut && intrIn {
					return true
				}
			}
		}
	}
	return false
}

// summarise turns the raw findings into a one-line verdict plus ordered
// remediation steps.
func summarise(d *Diagnostics) (string, []string) {
	var advice []string

	// The most specific diagnosis first: a Switch in a non-MTP homebrew USB mode.
	for _, dev := range d.Devices {
		if dev.IsNintendo && dev.ProductID == ProductHomebrewUSB {
			return "A Nintendo Switch is connected, but it is in a homebrew USB mode (DBIbackend, Awoo-Installer or GoldLeaf), not MTP.",
				[]string{
					"If DBI is running: press B to return to its main menu, press X, then choose \"Run MTP responder\".",
					"If Awoo-Installer or GoldLeaf is running: exit it and launch DBI instead -- SwitchMTP speaks MTP, not their USB protocols.",
					"Reconnect the cable after switching modes.",
				}
		}
	}

	if len(d.MTPDevices) > 0 {
		usable := 0
		for _, r := range d.MTPDevices {
			if r.Usable {
				usable++
			}
		}
		if usable > 0 {
			return "Found " + itoa(int64(usable)) + " usable MTP device(s).", nil
		}
	}

	if len(d.Blockers) > 0 {
		for _, b := range d.Blockers {
			advice = append(advice, b.Advice+" (PID "+itoa(int64(b.PID))+")")
		}
		advice = append(advice, "Then unplug the Switch and plug it back in.")
		return "Another process on " + platformHostNoun + " has claimed the USB device.", advice
	}

	if d.NintendoSeen {
		return "A Nintendo device is connected but no MTP interface was found.",
			append([]string{
				"On the Switch, make sure DBI's MTP responder is running (main menu, press X, \"Run MTP responder\").",
				"Try a different USB-C cable -- many cables carry power only.",
				"Unplug and reconnect the Switch.",
			}, platformNoInterfaceAdvice...)
	}

	if len(d.Devices) == 0 {
		return "No USB devices are visible at all.",
			append([]string{"SwitchMTP could not enumerate USB."}, platformNoUSBAdvice...)
	}

	return "No Nintendo Switch was detected.",
		[]string{
			"Connect the Switch with a USB-C data cable (charge-only cables will not work).",
			"Launch DBI on the Switch, press X, and choose \"Run MTP responder\".",
			"If the Switch is docked, try connecting it directly to " + platformHostNoun + " instead.",
		}
}
