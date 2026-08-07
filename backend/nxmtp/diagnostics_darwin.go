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
	"sort"
	"strings"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// platformHostNoun names the host in user-facing advice.
const platformHostNoun = "this Mac"

// platformNoInterfaceAdvice and platformNoUSBAdvice add macOS-specific steps to
// the shared advice in summarise.
var (
	platformNoInterfaceAdvice = []string{}
	platformNoUSBAdvice       = []string{
		"Check that the app has USB access, then restart it.",
	}
)

// knownBlockers are the macOS processes that routinely claim the PTP/MTP
// interface before we can, which makes libusb_claim_interface fail. This is by
// far the most common reason a Switch "is not detected" on a Mac.
var knownBlockers = map[string]string{
	// The daemon is "ptpcamerad" -- note the trailing d. macOS relaunches it
	// on demand for any still-image class interface, which is exactly what
	// DBI's MTP responder presents, so it is nearly always holding the Switch.
	// SwitchMTP takes the interface back automatically by re-enumerating the
	// port, so this is only reported when that recovery has already failed.
	"ptpcamerad":                  "macOS's PTP daemon has claimed the Switch. SwitchMTP normally takes it back automatically; if this persists, unplug the Switch, quit Image Capture and Photos, then reconnect.",
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

// interfaceHolders reports the processes holding a USB *interface* on an
// attached Nintendo device. Only interface-level clients can stop us claiming
// it; device-level clients (browsers enumerating WebUSB, peripheral utilities)
// are harmless and deliberately excluded, because naming them sends users
// chasing conflicts that do not exist.
func interfaceHolders(d *Diagnostics) []USBClient {
	seen := make(map[int]bool)
	for _, b := range d.Blockers {
		seen[b.PID] = true
	}

	var out []USBClient
	for _, dev := range d.Devices {
		if !dev.IsNintendo {
			continue
		}
		for _, o := range mtp.FindUSBOccupants(int(dev.VendorID), int(dev.ProductID)) {
			if !o.Blocking || seen[o.PID] || o.PID == d.SelfPID {
				continue
			}
			seen[o.PID] = true

			c := USBClient{PID: o.PID, Name: o.Name, Blocker: true}
			if advice, ok := knownBlockers[strings.ToLower(o.Name)]; ok {
				c.Known, c.Advice = true, advice
			} else {
				c.Advice = "\"" + o.Name + "\" is holding the Switch's USB interface. Quit it, then reconnect the Switch."
			}
			out = append(out, c)
		}
	}
	return out
}
