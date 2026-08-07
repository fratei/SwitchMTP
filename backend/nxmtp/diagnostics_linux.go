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

//go:build linux

package nxmtp

// platformHostNoun names the host in user-facing advice.
const platformHostNoun = "this computer"

// platformNoInterfaceAdvice and platformNoUSBAdvice add Linux-specific steps to
// the shared advice in summarise. The udev rule is the Linux analogue of the
// macOS USB-access prompt: without it /dev/bus/usb is root-only, so the device
// enumerates but cannot be opened.
var (
	platformNoInterfaceAdvice = []string{
		"Check that the SwitchMTP udev rule is installed in /etc/udev/rules.d/69-switchmtp.rules, then unplug and reconnect.",
	}
	platformNoUSBAdvice = []string{
		"Check that your user can read /dev/bus/usb -- SwitchMTP needs its udev rule installed in /etc/udev/rules.d/69-switchmtp.rules.",
		"Then restart SwitchMTP.",
	}
)

// knownBlockers are the Linux processes that routinely claim an MTP device
// before we can. The desktop MTP backends are the Linux equivalent of macOS's
// ptpcamerad: they auto-mount any MTP device the moment a file manager sees it.
//
// Note that libusb_detach_kernel_driver does NOT help here -- these are all
// userspace processes holding their own libusb handle, not kernel drivers. The
// durable fix is a udev rule setting ENV{ID_MTP_DEVICE}="" so they never claim
// the Switch in the first place.
var knownBlockers = map[string]string{
	"gvfsd-mtp":                   "GNOME's MTP backend has claimed the Switch. Quit it (`systemctl --user stop gvfs-mtp-volume-monitor`, or install the SwitchMTP udev rule to stop it claiming the device), then reconnect.",
	"gvfs-mtp-volume":             "GNOME's MTP volume monitor has claimed the Switch. Install the SwitchMTP udev rule to stop it claiming the device, then reconnect.",
	"gvfs-mtp-volume-monitor":     "GNOME's MTP volume monitor has claimed the Switch. Install the SwitchMTP udev rule to stop it claiming the device, then reconnect.",
	"gvfsd-gphoto2":               "GNOME's gphoto2 backend has claimed the Switch. Install the SwitchMTP udev rule to stop it claiming the device, then reconnect.",
	"gvfs-gphoto2-vo":             "GNOME's gphoto2 volume monitor has claimed the Switch. Install the SwitchMTP udev rule to stop it claiming the device, then reconnect.",
	"gvfs-gphoto2-volume-monitor": "GNOME's gphoto2 volume monitor has claimed the Switch. Install the SwitchMTP udev rule to stop it claiming the device, then reconnect.",
	"kiod5":                       "KDE's kio-mtp worker has claimed the Switch. Close any Dolphin window showing the device, then reconnect.",
	"kio_mtp":                     "KDE's kio-mtp worker has claimed the Switch. Close any Dolphin window showing the device, then reconnect.",
	"kio-mtp":                     "KDE's kio-mtp worker has claimed the Switch. Close any Dolphin window showing the device, then reconnect.",
	"mtp-probe":                   "udev's mtp-probe is inspecting the device. This is transient -- wait a moment and retry.",
	"jmtpfs":                      "jmtpfs has the device mounted. Unmount it (`fusermount -u <mountpoint>`), then reconnect.",
	"simple-mtpfs":                "simple-mtpfs has the device mounted. Unmount it (`fusermount -u <mountpoint>`), then reconnect.",
	"go-mtpfs":                    "go-mtpfs has the device mounted. Unmount it, then reconnect.",
	"mtpfs":                       "An MTP FUSE filesystem has the device mounted. Unmount it, then reconnect.",
	"mtp-server":                  "Another MTP client is holding the device.",
	"gphoto2":                     "gphoto2 is holding the device.",
	"gvfsd":                       "A GVFS backend has claimed the Switch. Install the SwitchMTP udev rule to stop it claiming the device, then reconnect.",
	"openmtp":                     "OpenMTP has an MTP session open. Quit it, then reconnect.",
}

// usbClients lists processes currently holding an open handle on a USB device.
//
// Linux has no API for this, but it does not need one: every libusb handle is
// an open file descriptor on /dev/bus/usb/<bus>/<device>, so scanning /proc/*/fd
// finds them all. The scan itself lives in procscan.go, untagged, so it can be
// tested against a fixture tree; only these two paths are Linux-specific.
func usbClients(selfPID int) []USBClient {
	return scanProc(procScan{
		procRoot:  "/proc",
		usbPrefix: "/dev/bus/usb/",
		selfPID:   selfPID,
		blockers:  knownBlockers,
	})
}

// interfaceHolders has no Linux equivalent: /proc tells us which processes hold
// a USB device, but not which interface within it. usbClients above is already
// device-scoped rather than machine-wide, so the extra precision macOS needs is
// not required here.
func interfaceHolders(d *Diagnostics) []USBClient { return nil }
