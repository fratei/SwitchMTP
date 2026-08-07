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

package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

// The MTP-capable flag is a structural test for the bulk-in/bulk-out/
// interrupt-in endpoint trio, mirroring the candidate filter in the underlying
// MTP library. Plenty of non-MTP hardware has that layout — USB ethernet
// adapters especially — so the column must say what it actually measures
// rather than assert the device is MTP. Pinned by a test.
const (
	doctorDeviceTableHeading = "MTP CANDIDATE"
	doctorDeviceTableNote    = `"MTP candidate" means the device has the USB endpoint layout MTP uses.
Other hardware shares that layout, so a "yes" here is not a Switch.`
)

// cmdDoctor explains why the device cannot be reached.
//
// This is the command that earns its keep on Linux. macOS has exactly one
// well-known culprit (ptpcamerad); Linux has several — gvfs, kio-mtp, jmtpfs —
// plus a permissions story that does not exist on macOS at all. Getting a
// straight answer without this would mean reading lsusb output and guessing.
func cmdDoctor(opts options, args []string) error {
	if len(args) > 0 {
		return usagef("doctor takes no arguments")
	}

	d := nxmtp.CollectDiagnostics()
	udev := checkUdevRule()

	if opts.json {
		return emitJSON(struct {
			*nxmtp.Diagnostics
			UdevRule *udevStatus `json:"udevRule,omitempty"`
		}{d, udev})
	}

	fmt.Printf("SwitchMTP %s on %s/%s\n\n", version, d.Platform, d.Arch)

	fmt.Printf("%s\n", d.Summary)

	if len(d.Advice) > 0 {
		fmt.Println("\nWhat to try:")
		for _, a := range d.Advice {
			fmt.Printf("  • %s\n", strings.ReplaceAll(a, "\n", "\n    "))
		}
	}

	if len(d.MTPDevices) > 0 {
		fmt.Println("\nMTP devices:")
		for _, r := range d.MTPDevices {
			state := "ready"
			if !r.Usable {
				state = "not usable"
			}
			fmt.Printf("  %-28s %s  (%s)\n", r.DisplayName, r.ID(), state)
		}
	}

	if len(d.Devices) > 0 {
		fmt.Println("\nUSB devices seen:")
		t := newTable("VID:PID", "CLASS", doctorDeviceTableHeading, "DESCRIPTION")
		// Nintendo hardware first: in a bug report attached to a list of two
		// dozen hubs and webcams, the one device that matters should not need
		// hunting for.
		devices := make([]nxmtp.USBDeviceSummary, len(d.Devices))
		copy(devices, d.Devices)
		sort.SliceStable(devices, func(i, j int) bool {
			return devices[i].IsNintendo && !devices[j].IsNintendo
		})
		for _, dev := range devices {
			mtpFlag := "no"
			if dev.MTPCapable {
				mtpFlag = "yes"
			}
			desc := dev.Description
			if desc == "" {
				// nxmtp only describes Nintendo hardware, so fall back to the
				// USB base class. "hub" or "vendor-specific" is far more
				// useful evidence than a blank cell.
				desc = usbClassName(dev.Class)
			}
			if dev.IsNintendo {
				desc = "★ " + desc
			}
			t.add(fmt.Sprintf("%04x:%04x", dev.VendorID, dev.ProductID),
				fmt.Sprintf("%02x", dev.Class), mtpFlag, desc)
		}
		t.render(os.Stdout)
		fmt.Println("\n" + doctorDeviceTableNote)
	}

	if len(d.Blockers) > 0 {
		fmt.Println("\nProcesses holding USB devices:")
		for _, b := range d.Blockers {
			label := b.Name
			if b.IsSelf {
				label += " (this process)"
			}
			fmt.Printf("  pid %-7d %s\n", b.PID, label)
			if b.Advice != "" {
				fmt.Printf("             %s\n", strings.ReplaceAll(b.Advice, "\n", "\n             "))
			}
		}
	}

	if udev != nil {
		fmt.Printf("\nudev rule: %s\n", udev.Detail)
		if !udev.Installed {
			fmt.Print(udevInstallInstructions)
		}
	}

	return nil
}

// udevStatus reports whether the host has the permissions rule installed.
type udevStatus struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Detail    string `json:"detail"`
}

// usbClassName names a USB base class code.
//
// Class 0x00 means "look at the interface descriptors instead", which is the
// commonest value and the reason a raw hex code alone tells the reader almost
// nothing.
func usbClassName(class uint8) string {
	switch class {
	case 0x00:
		return "per-interface"
	case 0x01:
		return "audio"
	case 0x02:
		return "communications"
	case 0x03:
		return "human interface"
	case 0x05:
		return "physical"
	case 0x06:
		return "still imaging (PTP/MTP)"
	case 0x07:
		return "printer"
	case 0x08:
		return "mass storage"
	case 0x09:
		return "hub"
	case 0x0a:
		return "CDC data"
	case 0x0b:
		return "smart card"
	case 0x0e:
		return "video"
	case 0xdc:
		return "diagnostic"
	case 0xe0:
		return "wireless"
	case 0xef:
		return "miscellaneous"
	case 0xfe:
		return "application specific"
	case 0xff:
		return "vendor specific"
	default:
		return "class " + fmt.Sprintf("0x%02x", class)
	}
}

// udevRulePaths are the locations a rule may legitimately live in: the
// admin-owned directory first, then the package-owned one.
var udevRulePaths = []string{
	"/etc/udev/rules.d/69-switchmtp.rules",
	"/lib/udev/rules.d/69-switchmtp.rules",
	"/usr/lib/udev/rules.d/69-switchmtp.rules",
}

// checkUdevRule looks for the permissions rule. It returns nil off Linux,
// where the concept does not exist.
func checkUdevRule() *udevStatus {
	if runtime.GOOS != "linux" {
		return nil
	}
	for _, p := range udevRulePaths {
		if _, err := os.Stat(p); err == nil {
			return &udevStatus{Installed: true, Path: p, Detail: "installed at " + p}
		}
	}

	// A rule from another Switch tool grants the same access, so report it
	// rather than telling the user to install a redundant one.
	if other := findForeignNintendoRule(); other != "" {
		return &udevStatus{
			Installed: true,
			Path:      other,
			Detail:    "not installed, but " + other + " already covers Nintendo devices",
		}
	}

	return &udevStatus{Detail: "not installed — USB access will need root, and gvfs may claim the device first"}
}

// findForeignNintendoRule scans the admin rules directory for any rule
// mentioning Nintendo's vendor id.
func findForeignNintendoRule() string {
	entries, err := os.ReadDir("/etc/udev/rules.d")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".rules") {
			continue
		}
		p := "/etc/udev/rules.d/" + e.Name()
		// Rules are tiny; reading them whole is fine and simpler than
		// streaming.
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(b)), "057e") {
			return p
		}
	}
	return ""
}

const udevInstallInstructions = `
If you have a checkout of the repository:

  sudo cp packaging/linux/69-switchmtp.rules /etc/udev/rules.d/
  sudo udevadm control --reload-rules && sudo udevadm trigger

Otherwise paste this (one time, needs sudo):

  sudo tee /etc/udev/rules.d/69-switchmtp.rules >/dev/null <<'EOF'
  # Nintendo Switch, MTP mode (DBI or Horizon OS).
  SUBSYSTEM=="usb", ATTR{idVendor}=="057e", ATTR{idProduct}=="201d", TAG+="uaccess", GROUP="plugdev", MODE="0660", ENV{ID_MTP_DEVICE}="", ENV{ID_GPHOTO2}=""
  # Nintendo Switch 2, MTP mode.
  SUBSYSTEM=="usb", ATTR{idVendor}=="057e", ATTR{idProduct}=="2061", TAG+="uaccess", GROUP="plugdev", MODE="0660", ENV{ID_MTP_DEVICE}="", ENV{ID_GPHOTO2}=""
  # Nintendo Switch in a homebrew USB mode, so it can at least be detected.
  SUBSYSTEM=="usb", ATTR{idVendor}=="057e", ATTR{idProduct}=="3000", TAG+="uaccess", GROUP="plugdev", MODE="0660"
  EOF
  sudo udevadm control --reload-rules && sudo udevadm trigger

Then unplug and reconnect the console.

TAG+="uaccess" grants the logged-in user access; GROUP="plugdev" covers Debian,
Ubuntu and SSH sessions. Clearing ID_MTP_DEVICE stops gvfs and kio-mtp claiming
the device before SwitchMTP can.
`
