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

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

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
// finds them all. Processes owned by other users are silently skipped (their fd
// directories are unreadable without root), which is fine -- the blockers that
// matter, the desktop MTP backends, run as the same user we do.
func usbClients(selfPID int) []USBClient {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	var out []USBClient
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if !holdsUSBDevice(pid) {
			continue
		}

		name := processName(pid)
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

// holdsUSBDevice reports whether a process has any /dev/bus/usb file descriptor
// open. Unreadable fd directories mean "another user's process", not "no".
func holdsUSBDevice(pid int) bool {
	fdDir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return false
	}
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, "/dev/bus/usb/") {
			return true
		}
	}
	return false
}

// processName reads a process's short name, preferring /proc/<pid>/comm.
func processName(pid int) string {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return "pid " + strconv.Itoa(pid)
	}
	name := strings.TrimSpace(string(raw))
	if name == "" {
		return "pid " + strconv.Itoa(pid)
	}
	return name
}

// interfaceHolders has no Linux equivalent: /proc tells us which processes hold
// a USB device, but not which interface within it. usbClients above is already
// device-scoped rather than machine-wide, so the extra precision macOS needs is
// not required here.
func interfaceHolders(d *Diagnostics) []USBClient { return nil }
