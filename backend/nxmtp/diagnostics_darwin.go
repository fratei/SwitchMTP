//go:build darwin

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

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

typedef struct {
	int  pid;
	char process_name[256];
} SwitchMTPUSBClient;

// switchmtp_find_usb_clients walks the IOService plane looking for USB user
// clients, which is how macOS represents "some process has claimed this USB
// device or interface". Each match carries an IOUserClientCreator property of
// the form "pid 623, SomeProcess", which is the only supported way to learn
// who is holding a device.
//
// This is the same technique ioreg uses. Returns the total number of matches;
// writes at most maxResults of them.
static int switchmtp_find_usb_clients(SwitchMTPUSBClient* results, int maxResults, int* outWritten) {
	int total = 0, written = 0;
	io_iterator_t iter;

	kern_return_t kr = IORegistryCreateIterator(
		0, kIOServicePlane, kIORegistryIterateRecursively, &iter);
	if (kr != KERN_SUCCESS) {
		*outWritten = 0;
		return 0;
	}

	io_object_t entry;
	while ((entry = IOIteratorNext(iter)) != IO_OBJECT_NULL) {
		io_name_t className;
		if (IOObjectGetClass(entry, className) == KERN_SUCCESS &&
		    (strcmp(className, "AppleUSBHostDeviceUserClient") == 0 ||
		     strcmp(className, "AppleUSBHostInterfaceUserClient") == 0)) {

			CFTypeRef prop = IORegistryEntryCreateCFProperty(
				entry, CFSTR("IOUserClientCreator"), kCFAllocatorDefault, 0);
			if (prop) {
				if (CFGetTypeID(prop) == CFStringGetTypeID()) {
					char buf[512];
					if (CFStringGetCString((CFStringRef)prop, buf, sizeof(buf), kCFStringEncodingUTF8)) {
						int pid = 0;
						char name[256] = {0};
						if (sscanf(buf, "pid %d, %255s", &pid, name) == 2) {
							total++;
							if (written < maxResults) {
								results[written].pid = pid;
								strncpy(results[written].process_name, name, 255);
								results[written].process_name[255] = '\0';
								written++;
							}
						}
					}
				}
				CFRelease(prop);
			}
		}
		IOObjectRelease(entry);
	}
	IOObjectRelease(iter);
	*outWritten = written;
	return total;
}
*/
import "C"

import (
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/ganeshrvel/usb"
)

// USBClient is a process holding a USB device or interface.
type USBClient struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`
	Known   bool   `json:"known"`
	Advice  string `json:"advice,omitempty"`
	IsSelf  bool   `json:"isSelf"`
	Blocker bool   `json:"blocker"`
}

// knownBlockers are the macOS processes that routinely claim the PTP/MTP
// interface before we can, which makes libusb_claim_interface fail. This is by
// far the most common reason a Switch "is not detected" on a Mac.
var knownBlockers = map[string]string{
	"ptpcamera":                   "macOS's built-in camera driver has claimed the Switch. Quit Image Capture, Photos and Preview, then reconnect.",
	"imagecaptureextension":       "Image Capture Extension has claimed the Switch. Quit Image Capture and any photo import prompt, then reconnect.",
	"imagecaptureextension2":      "Image Capture Extension has claimed the Switch. Quit Image Capture and any photo import prompt, then reconnect.",
	"androidfiletransferagent":    "Android File Transfer Agent claims every MTP device. Quit it (and remove it from Login Items) then reconnect.",
	"android file transfer agent": "Android File Transfer Agent claims every MTP device. Quit it (and remove it from Login Items) then reconnect.",
	"androidfiletransfer":         "Android File Transfer claims every MTP device. Quit it, then reconnect.",
	"openmtp":                     "OpenMTP has an MTP session open. Quit it, then reconnect.",
	"gphoto2":                     "gphoto2 is holding the device.",
	"mtp-server":                  "Another MTP client is holding the device.",
	"simple-mtpfs":                "Another MTP client is holding the device.",
}

// USBDeviceSummary is one raw USB device, used to show what the Mac can see
// even when we cannot claim it.
type USBDeviceSummary struct {
	VendorID    uint16 `json:"vendorId"`
	ProductID   uint16 `json:"productId"`
	Class       uint8  `json:"deviceClass"`
	IsNintendo  bool   `json:"isNintendo"`
	Description string `json:"description"`
	MTPCapable  bool   `json:"mtpCapable"`
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

	if refs, err := FindDevices(); err == nil {
		d.MTPDevices = refs
	}

	d.Summary, d.Advice = summarise(d)
	return d
}

// usbClients lists processes currently holding USB user clients.
func usbClients(selfPID int) []USBClient {
	const maxResults = 128
	var raw [maxResults]C.SwitchMTPUSBClient
	var written C.int

	C.switchmtp_find_usb_clients(&raw[0], C.int(maxResults), &written)

	seen := make(map[int]bool)
	out := make([]USBClient, 0, int(written))
	for i := 0; i < int(written); i++ {
		pid := int(raw[i].pid)
		if seen[pid] {
			continue
		}
		seen[pid] = true

		name := C.GoString(&raw[i].process_name[0])
		name = strings.TrimSuffix(strings.TrimSpace(name), ",")

		c := USBClient{PID: pid, Name: name, IsSelf: pid == selfPID}
		if advice, ok := knownBlockers[strings.ToLower(name)]; ok && !c.IsSelf {
			c.Known, c.Blocker, c.Advice = true, true, advice
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Blocker != out[j].Blocker {
			return out[i].Blocker
		}
		return out[i].PID < out[j].PID
	})
	return out
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
		return "Another process on this Mac has claimed the USB device.", advice
	}

	if d.NintendoSeen {
		return "A Nintendo device is connected but no MTP interface was found.",
			[]string{
				"On the Switch, make sure DBI's MTP responder is running (main menu, press X, \"Run MTP responder\").",
				"Try a different USB-C cable -- many cables carry power only.",
				"Unplug and reconnect the Switch.",
			}
	}

	if len(d.Devices) == 0 {
		return "No USB devices are visible at all.",
			[]string{"SwitchMTP could not enumerate USB. Check that the app has USB access, then restart it."}
	}

	return "No Nintendo Switch was detected.",
		[]string{
			"Connect the Switch with a USB-C data cable (charge-only cables will not work).",
			"Launch DBI on the Switch, press X, and choose \"Run MTP responder\".",
			"If the Switch is docked, try connecting it directly to the Mac instead.",
		}
}
