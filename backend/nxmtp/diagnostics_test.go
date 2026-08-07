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
	"strings"
	"testing"

	"github.com/ganeshrvel/usb"
)

// summarise is the part of diagnostics users actually read, and it is now
// shared by every platform. These tests pin its priority order, because the
// ordering is the whole point: a Switch in homebrew USB mode must never be
// reported as "no Switch detected", and a blocked device must never be
// reported as a cabling problem.

func TestSummariseHomebrewModeBeatsEverythingElse(t *testing.T) {
	// A Switch in DBIbackend/Awoo/GoldLeaf mode enumerates as a Nintendo device
	// that is not MTP. It is also, confusingly, a state where blockers may be
	// present. The homebrew diagnosis has to win, because it is the only one
	// that tells the user the actual fix.
	d := &Diagnostics{
		Devices: []USBDeviceSummary{
			{VendorID: VendorNintendo, ProductID: ProductHomebrewUSB, IsNintendo: true},
		},
		NintendoSeen: true,
		Blockers:     []USBClient{{PID: 42, Name: "gvfsd-mtp", Advice: "quit it"}},
	}

	summary, advice := summarise(d)

	if !strings.Contains(summary, "homebrew USB mode") {
		t.Fatalf("homebrew mode not diagnosed, got: %q", summary)
	}
	if len(advice) == 0 {
		t.Fatal("homebrew diagnosis gave no remediation steps")
	}
	if !strings.Contains(strings.Join(advice, " "), "Run MTP responder") {
		t.Errorf("advice does not tell the user how to switch DBI to MTP: %v", advice)
	}
}

func TestSummariseUsableDeviceReportsSuccessWithNoAdvice(t *testing.T) {
	d := &Diagnostics{
		MTPDevices: []DeviceRef{{Usable: true}, {Usable: false}},
	}

	summary, advice := summarise(d)

	if !strings.Contains(summary, "1 usable") {
		t.Errorf("expected a count of usable devices, got: %q", summary)
	}
	if advice != nil {
		t.Errorf("a working device should produce no advice, got: %v", advice)
	}
}

func TestSummariseBlockersNameTheProcessAndItsPID(t *testing.T) {
	// The PID matters: it is what lets a user confirm they killed the right
	// thing. Losing it would make the advice much harder to act on.
	d := &Diagnostics{
		NintendoSeen: true,
		Devices: []USBDeviceSummary{
			{VendorID: VendorNintendo, ProductID: ProductSwitchMTP, IsNintendo: true},
		},
		Blockers: []USBClient{{PID: 1234, Name: "gvfsd-mtp", Advice: "Quit GVFS."}},
	}

	summary, advice := summarise(d)

	if !strings.Contains(summary, platformHostNoun) {
		t.Errorf("summary should name the host, got: %q", summary)
	}
	joined := strings.Join(advice, "\n")
	if !strings.Contains(joined, "Quit GVFS.") || !strings.Contains(joined, "PID 1234") {
		t.Errorf("blocker advice lost its text or PID: %v", advice)
	}
	if !strings.Contains(joined, "unplug the Switch") {
		t.Errorf("advice should end with the reconnect step: %v", advice)
	}
}

func TestSummariseNintendoWithoutMTPInterface(t *testing.T) {
	d := &Diagnostics{
		NintendoSeen: true,
		Devices: []USBDeviceSummary{
			{VendorID: VendorNintendo, ProductID: 0x2000, IsNintendo: true},
		},
	}

	summary, advice := summarise(d)

	if !strings.Contains(summary, "no MTP interface") {
		t.Errorf("unexpected summary: %q", summary)
	}
	// Every platform adds its own extra steps here; the shared three must
	// always survive that append.
	if len(advice) < 3 {
		t.Fatalf("expected at least the three shared steps, got: %v", advice)
	}
	if !strings.Contains(strings.Join(advice, " "), "cable") {
		t.Errorf("advice should mention the charge-only cable trap: %v", advice)
	}
}

func TestSummariseNoUSBAtAllIsDistinctFromNoSwitch(t *testing.T) {
	// These two look similar but mean opposite things: one is "we cannot see
	// USB", the other is "USB works, the Switch is not there". Conflating them
	// sends users down the wrong path.
	blind, blindAdvice := summarise(&Diagnostics{})
	present, presentAdvice := summarise(&Diagnostics{
		Devices: []USBDeviceSummary{{VendorID: 0x05ac, ProductID: 0x0001}},
	})

	if !strings.Contains(blind, "No USB devices are visible") {
		t.Errorf("unexpected summary with no devices: %q", blind)
	}
	if len(blindAdvice) == 0 {
		t.Error("a total USB failure should still offer a next step")
	}
	if !strings.Contains(present, "No Nintendo Switch was detected") {
		t.Errorf("unexpected summary with unrelated devices: %q", present)
	}
	if blind == present {
		t.Error("the two failure modes must not produce the same summary")
	}
	if !strings.Contains(strings.Join(presentAdvice, " "), "data cable") {
		t.Errorf("advice should mention needing a data cable: %v", presentAdvice)
	}
}

func TestDescribeNintendoCoversEveryKnownMode(t *testing.T) {
	cases := map[uint16]string{
		ProductSwitchMTP:   "MTP mode",
		ProductSwitch2MTP:  "Switch 2",
		ProductHomebrewUSB: "NOT MTP",
		0xFFFF:             "non-MTP USB mode",
	}
	for pid, want := range cases {
		if got := describeNintendo(pid); !strings.Contains(got, want) {
			t.Errorf("describeNintendo(%#04x) = %q, want it to contain %q", pid, got, want)
		}
	}
}

func TestPlatformAdviceIsWellFormed(t *testing.T) {
	// Each platform supplies these. A missing host noun or an empty advice
	// string would produce sentences like "Another process on  has claimed...".
	if strings.TrimSpace(platformHostNoun) == "" {
		t.Error("platformHostNoun is empty")
	}
	for _, s := range append(append([]string{}, platformNoInterfaceAdvice...), platformNoUSBAdvice...) {
		if strings.TrimSpace(s) == "" {
			t.Error("platform advice contains an empty string")
		}
	}
	for name, advice := range knownBlockers {
		if strings.TrimSpace(name) == "" {
			t.Error("knownBlockers has an empty key")
		}
		if strings.TrimSpace(advice) == "" {
			t.Errorf("knownBlockers[%q] has no advice", name)
		}
		if name != strings.ToLower(name) {
			// Lookups lowercase the process name, so a mixed-case key can
			// never match.
			t.Errorf("knownBlockers key %q is not lowercase and can never match", name)
		}
	}
}

func TestCollectDiagnosticsReportsHostIdentity(t *testing.T) {
	// This is the call ffi/exports.go makes unconditionally, so it has to work
	// on every platform even with no device attached.
	d := CollectDiagnostics()
	if d == nil {
		t.Fatal("CollectDiagnostics returned nil")
	}
	if d.Platform == "" || d.Arch == "" {
		t.Errorf("platform/arch not populated: %+v", d)
	}
	if d.SelfPID <= 0 {
		t.Errorf("SelfPID not populated: %d", d.SelfPID)
	}
	if strings.TrimSpace(d.Summary) == "" {
		t.Error("diagnostics produced no summary")
	}
}

// TestEmptyDeviceListDoneDoesNotPanic guards a fix in the vendored libusb
// wrapper. Its DeviceList.Done() freed the list via &d[0], which panics on an
// empty list -- and libusb reports zero devices on any machine with no USB host
// controller. A Mac always has a root hub, so this only ever fires somewhere
// like a CI container, which is precisely where it is hardest to notice by hand.
func TestEmptyDeviceListDoneDoesNotPanic(t *testing.T) {
	var nilList usb.DeviceList
	nilList.Done()
	usb.DeviceList{}.Done()
}
