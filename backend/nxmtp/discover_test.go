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

import "testing"

// TestClassifyInterface pins the look-alike filter down with the two real
// interfaces observed on the development machine, plus the vendor-specific MTP
// shape used by Android handsets.
//
// The RTL8156 case is the reason the filter exists: a USB Ethernet adapter
// presents one bulk-in, one bulk-out and one interrupt-in endpoint, which is
// exactly the layout mtp.FindDevices matches on, so it used to appear in the
// device list beside the Switch.
func TestClassifyInterface(t *testing.T) {
	cases := []struct {
		name                 string
		class                byte
		subClass             byte
		protocol             byte
		stringIndex          byte
		wantAccept           bool
		wantNeedsStringCheck bool
	}{
		{
			name:  "DBI still image PTP (057e:201d)",
			class: 6, subClass: 1, protocol: 1, stringIndex: 4,
			wantAccept: true, wantNeedsStringCheck: false,
		},
		{
			name:  "Realtek RTL8156 ethernet (0bda:8156)",
			class: 255, subClass: 255, protocol: 0, stringIndex: 0,
			wantAccept: false, wantNeedsStringCheck: false,
		},
		{
			name:  "Android MTP-only, vendor specific with a string",
			class: 255, subClass: 255, protocol: 0, stringIndex: 5,
			wantAccept: true, wantNeedsStringCheck: true,
		},
		{
			name:  "still image with odd subclass is still accepted",
			class: 6, subClass: 0, protocol: 0, stringIndex: 0,
			wantAccept: true, wantNeedsStringCheck: false,
		},
		{
			name:  "CDC data",
			class: 10, subClass: 0, protocol: 0, stringIndex: 3,
			wantAccept: false, wantNeedsStringCheck: false,
		},
		{
			name:  "mass storage",
			class: 8, subClass: 6, protocol: 80, stringIndex: 2,
			wantAccept: false, wantNeedsStringCheck: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			accept, needsStringCheck := classifyInterface(tc.class, tc.subClass, tc.protocol, tc.stringIndex)
			if accept != tc.wantAccept {
				t.Errorf("accept = %v, want %v", accept, tc.wantAccept)
			}
			if needsStringCheck != tc.wantNeedsStringCheck {
				t.Errorf("needsStringCheck = %v, want %v", needsStringCheck, tc.wantNeedsStringCheck)
			}
		})
	}
}
