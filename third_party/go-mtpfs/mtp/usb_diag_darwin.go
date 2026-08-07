//go:build darwin

package mtp

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

typedef struct {
	int pid;
	char process_name[256];
	// blocking is 1 when the client holds an *interface*, which is what
	// actually prevents us claiming it. Device-level clients (browsers doing
	// WebUSB enumeration, peripheral utilities scanning) are harmless and
	// must not be reported as the cause of a failure.
	int blocking;
} USBClientInfo;

// findClientsForDevice reports the processes holding user clients on ONE USB
// device, identified by its vendor and product IDs.
//
// MODIFIED (SwitchMTP): upstream walked the whole IOService plane and returned
// every USB user client on the machine. On a normal desktop that means web
// browsers, keyboards and stream decks -- none of which have anything to do
// with the device we failed to open. Worse, the genuine culprit (ptpcamerad)
// was pushed out of the fixed-size result buffer by that noise, so the one
// process the user needed to know about was the one they never saw.
//
// We now locate the matching IOUSBHostDevice first and search only its
// subtree, which is exactly the set of clients that can block us.
static int findClientsForDevice(int vendorID, int productID,
                                USBClientInfo* results, int maxResults, int* outWritten) {
	int total = 0;
	int written = 0;
	*outWritten = 0;

	CFMutableDictionaryRef matching = IOServiceMatching("IOUSBHostDevice");
	if (!matching) return 0;

	io_iterator_t devIter;
	if (IOServiceGetMatchingServices(kIOMainPortDefault, matching, &devIter) != KERN_SUCCESS) {
		return 0;
	}

	io_object_t device;
	while ((device = IOIteratorNext(devIter)) != IO_OBJECT_NULL) {
		int vid = 0, pid = 0;
		CFTypeRef vidRef = IORegistryEntryCreateCFProperty(device, CFSTR("idVendor"), kCFAllocatorDefault, 0);
		CFTypeRef pidRef = IORegistryEntryCreateCFProperty(device, CFSTR("idProduct"), kCFAllocatorDefault, 0);
		if (vidRef && CFGetTypeID(vidRef) == CFNumberGetTypeID()) CFNumberGetValue((CFNumberRef)vidRef, kCFNumberIntType, &vid);
		if (pidRef && CFGetTypeID(pidRef) == CFNumberGetTypeID()) CFNumberGetValue((CFNumberRef)pidRef, kCFNumberIntType, &pid);
		if (vidRef) CFRelease(vidRef);
		if (pidRef) CFRelease(pidRef);

		if (vid != vendorID || pid != productID) {
			IOObjectRelease(device);
			continue;
		}

		// Walk this device's subtree: its interfaces and their user clients.
		io_iterator_t childIter;
		if (IORegistryEntryCreateIterator(device, kIOServicePlane,
		        kIORegistryIterateRecursively, &childIter) == KERN_SUCCESS) {
			io_object_t entry;
			while ((entry = IOIteratorNext(childIter)) != IO_OBJECT_NULL) {
				io_name_t className;
				IOObjectGetClass(entry, className);
				if (strstr(className, "UserClient") != NULL) {
					int blocking = (strstr(className, "Interface") != NULL) ? 1 : 0;
					CFTypeRef prop = IORegistryEntryCreateCFProperty(
						entry, CFSTR("IOUserClientCreator"), kCFAllocatorDefault, 0);
					if (prop && CFGetTypeID(prop) == CFStringGetTypeID()) {
						char buf[512];
						if (CFStringGetCString((CFStringRef)prop, buf, sizeof(buf), kCFStringEncodingUTF8)) {
							// Property value looks like: "pid 623, SomeProcess"
							int cpid = 0;
							char name[256] = {0};
							// %[^\n] rather than %s: process names contain
							// spaces ("Microsoft Edge"), and truncating at the
							// first one produces misleading diagnostics.
							if (sscanf(buf, "pid %d, %255[^\n]", &cpid, name) == 2) {
								total++;
								if (written < maxResults) {
									results[written].pid = cpid;
									strncpy(results[written].process_name, name, 255);
									results[written].process_name[255] = '\0';
									results[written].blocking = blocking;
									written++;
								}
							}
						}
					}
					if (prop) CFRelease(prop);
				}
				IOObjectRelease(entry);
			}
			IOObjectRelease(childIter);
		}
		IOObjectRelease(device);
	}
	IOObjectRelease(devIter);

	*outWritten = written;
	return total;
}
*/
import "C"

import (
	"fmt"
	"log"
	"strings"
)

// USBOccupant is a process holding a user client on the device we wanted.
type USBOccupant struct {
	PID  int
	Name string
	// Blocking reports whether this process holds the USB *interface*, which
	// is what stops us claiming it. Processes holding only a device-level
	// client coexist with us happily and are recorded but not blamed.
	Blocking bool
}

// SystemPTPDaemon is macOS's own PTP/camera daemon. It binds every still-image
// class interface the moment it enumerates, which includes DBI's MTP responder,
// and it holds that interface exclusively. It is the single most common reason
// a Switch fails to open on macOS, so it is called out by name.
const SystemPTPDaemon = "ptpcamerad"

// FindUSBOccupants reports the processes holding user clients on the USB device
// with the given vendor and product IDs, excluding our own processes.
func FindUSBOccupants(vendorID, productID int) []USBOccupant {
	const maxResults = 64
	var results [maxResults]C.USBClientInfo

	var written C.int
	total := int(C.findClientsForDevice(C.int(vendorID), C.int(productID),
		&results[0], C.int(maxResults), &written))
	if total > int(written) {
		log.Printf("USB diag: %d clients found but only %d reported", total, written)
	}

	seen := make(map[int]bool)
	var out []USBOccupant
	for i := 0; i < int(written); i++ {
		pid := int(results[i].pid)
		name := C.GoString(&results[i].process_name[0])
		if isOwnProcess(name) || seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, USBOccupant{PID: pid, Name: name, Blocking: results[i].blocking != 0})
	}
	return out
}

// getUSBOccupyingPIDs formats the occupants of the device currently being
// opened, for appending to an error message.
//
// MODIFIED (SwitchMTP): scoped to the device we are actually opening. Upstream
// listed every USB user client on the machine, which in practice meant naming
// browsers and keyboards while omitting the real culprit.
func (d *Device) getUSBOccupyingPIDs() string {
	desc, err := d.dev.GetDeviceDescriptor()
	if err != nil {
		return ""
	}
	occupants := FindUSBOccupants(int(desc.IdVendor), int(desc.IdProduct))
	if len(occupants) == 0 {
		return ""
	}

	var parts []string
	daemonHeld := false
	for _, o := range occupants {
		if !o.Blocking {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (pid %d)", o.Name, o.PID))
		if o.Name == SystemPTPDaemon {
			daemonHeld = true
		}
	}
	if len(parts) == 0 {
		return ""
	}
	msg := " -- interface held by " + strings.Join(parts, ", ")
	if daemonHeld {
		msg += "; macOS's PTP daemon claims still-image interfaces automatically"
	}
	log.Printf("USB diag:%s", msg)
	return msg
}

// isOwnProcess reports whether a USB client belongs to us. Reporting ourselves
// as an occupier would send users chasing a conflict that does not exist. The
// upstream fork hard-coded "SwiftMTP"; we match our own binaries instead.
func isOwnProcess(name string) bool {
	switch name {
	case "SwitchMTP", "switchmtp-cli", "switchmtp-doctor", "SwiftMTP":
		return true
	}
	return false
}
