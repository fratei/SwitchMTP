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
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

func TestHumanBytesUsesDecimalUnits(t *testing.T) {
	// Decimal, to agree with what the console and the SD card's packaging say.
	cases := map[int64]string{
		0:             "0 B",
		999:           "999 B",
		1000:          "1.00 kB",
		1500:          "1.50 kB",
		1_000_000:     "1.00 MB",
		15_500_000:    "15.5 MB",
		999_000_000:   "999 MB",
		1_000_000_000: "1.00 GB",
		// A typical Switch title, and the >4 GiB case the engine goes out of
		// its way to support.
		4_294_967_296:     "4.29 GB",
		17_000_000_000:    "17.0 GB",
		1_000_000_000_000: "1.00 TB",
		-1:                "—",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanDurationIsCompact(t *testing.T) {
	cases := map[time.Duration]string{
		0:                  "0s",
		5 * time.Second:    "5s",
		65 * time.Second:   "1m05s",
		3600 * time.Second: "1h00m00s",
		3725 * time.Second: "1h02m05s",
		-1:                 "—",
	}
	for in, want := range cases {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanRateHandlesTheUnknownCase(t *testing.T) {
	if got := humanRate(0); got != "—" {
		t.Errorf("humanRate(0) = %q, want the em dash placeholder", got)
	}
	if got := humanRate(20_000_000); !strings.HasSuffix(got, "/s") {
		t.Errorf("humanRate should carry a per-second suffix, got %q", got)
	}
}

// The indefinite case is the one that matters. While DBI commits an install
// the byte counter has already reached 100%, and drawing a full bar there is
// exactly what made the app look wedged. An indefinite update must show the
// note instead.
func TestFormatProgressIndefiniteShowsTheNoteNotAFullBar(t *testing.T) {
	line := formatProgress(nxmtp.Progress{
		Name:           "Game.nsp",
		Status:         nxmtp.StatusInstalling,
		Indefinite:     true,
		Note:           "The Switch is installing Game.nsp.",
		ActiveFileSize: nxmtp.FileSizeProgress{Total: 100, Sent: 100, Progress: 100},
	})
	if !strings.Contains(line, "The Switch is installing") {
		t.Errorf("indefinite progress should show the note, got %q", line)
	}
	if strings.Contains(line, "100.0%") {
		t.Errorf("indefinite progress must not show a percentage, got %q", line)
	}
	if !strings.Contains(line, "installing") {
		t.Errorf("indefinite progress should still name the phase, got %q", line)
	}
}

func TestFormatProgressShowsPositionInAMultiFileTransfer(t *testing.T) {
	line := formatProgress(nxmtp.Progress{
		Name:           "Second.nsp",
		Status:         nxmtp.StatusTransferring,
		TotalFiles:     3,
		FilesSent:      1,
		Speed:          100_000_000,
		ActiveFileSize: nxmtp.FileSizeProgress{Total: 1_000_000_000, Sent: 500_000_000, Progress: 50},
		BulkFileSize:   nxmtp.FileSizeProgress{Total: 3_000_000_000, Sent: 1_500_000_000, Progress: 50},
	})
	if !strings.Contains(line, "[2/3]") {
		t.Errorf("expected a [2/3] counter, got %q", line)
	}
	if !strings.Contains(line, "50.0%") {
		t.Errorf("expected a percentage, got %q", line)
	}
	if !strings.Contains(line, "ETA 15s") {
		t.Errorf("expected a 15s ETA from the bulk counters, got %q", line)
	}
}

// A sub-second remainder must not print "ETA 0s". This also pins the
// nanosecond-scaled arithmetic in estimate: converting to a Duration before
// multiplying truncates the ratio and silently loses all sub-minute accuracy.
func TestFormatProgressOmitsASubSecondETA(t *testing.T) {
	line := formatProgress(nxmtp.Progress{
		Name:         "Almost.nsp",
		Status:       nxmtp.StatusTransferring,
		Speed:        20_000_000,
		BulkFileSize: nxmtp.FileSizeProgress{Total: 1000, Sent: 500},
	})
	if strings.Contains(line, "ETA") {
		t.Errorf("a sub-second remainder should not show an ETA, got %q", line)
	}
}

// CurrentFile is authoritative when the engine supplies it; FilesSent+1 is
// only the fallback. They disagree during the gap between finishing a file and
// starting the next.
func TestFormatProgressPrefersCurrentFileOverFilesSent(t *testing.T) {
	line := formatProgress(nxmtp.Progress{
		Status:      nxmtp.StatusTransferring,
		TotalFiles:  5,
		FilesSent:   1,
		CurrentFile: 3,
	})
	if !strings.Contains(line, "[3/5]") {
		t.Errorf("expected [3/5] from CurrentFile, got %q", line)
	}
}

func TestFormatProgressClampsTheCounterToTheTotal(t *testing.T) {
	line := formatProgress(nxmtp.Progress{
		Status:     nxmtp.StatusTransferring,
		TotalFiles: 2,
		FilesSent:  2,
	})
	if strings.Contains(line, "[3/2]") {
		t.Errorf("counter should be clamped to the total, got %q", line)
	}
}

func TestEstimateUsesBulkCountersNotTheActiveFile(t *testing.T) {
	// Half of a 3 GB job remains at 100 MB/s, so ~15s — not the 5s a
	// per-file estimate would give.
	got := estimate(nxmtp.Progress{
		Speed:          100_000_000,
		ActiveFileSize: nxmtp.FileSizeProgress{Total: 1_000_000_000, Sent: 500_000_000},
		BulkFileSize:   nxmtp.FileSizeProgress{Total: 3_000_000_000, Sent: 1_500_000_000},
	})
	if got != 15*time.Second {
		t.Errorf("estimate = %v, want 15s (derived from the bulk counters)", got)
	}
}

func TestEstimateIsZeroWhenUnknowable(t *testing.T) {
	if got := estimate(nxmtp.Progress{Speed: 0}); got != 0 {
		t.Errorf("estimate with no speed = %v, want 0", got)
	}
	if got := estimate(nxmtp.Progress{
		Speed:        100,
		BulkFileSize: nxmtp.FileSizeProgress{Total: 100, Sent: 100},
	}); got != 0 {
		t.Errorf("estimate with nothing remaining = %v, want 0", got)
	}
}

func TestBarIsClampedAtBothEnds(t *testing.T) {
	for _, p := range []float64{-10, 0, 50, 100, 150} {
		got := bar(p)
		if n := len([]rune(got)); n != 22 {
			t.Errorf("bar(%v) has %d runes, want 22 (20 cells plus brackets): %q", p, n, got)
		}
	}
}

func TestTableAlignsColumnsAndCountsRunesNotBytes(t *testing.T) {
	tb := newTable("SIZE", "NAME").rightAlign(0)
	tb.add("1.00 GB", "ゼルダの伝説.nsp")
	tb.add("12 B", "short.txt")

	var buf bytes.Buffer
	tb.render(&buf)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected a header, a rule and two rows, got %d lines:\n%s", len(lines), buf.String())
	}
	// Right-aligned column: the shorter value is padded on the left.
	if !strings.HasPrefix(lines[3], "   12 B") {
		t.Errorf("expected the size column to be right-aligned, got %q", lines[3])
	}
	for i, l := range lines {
		if strings.HasSuffix(l, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i, l)
		}
	}
}

func TestTableRendersNothingWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	newTable("A", "B").render(&buf)
	if buf.Len() != 0 {
		t.Errorf("an empty table should render nothing, got %q", buf.String())
	}
}

func TestTruncateKeepsTheEndOfTheName(t *testing.T) {
	// The extension and the distinguishing part of a game name are at the end,
	// so that is the half worth keeping.
	got := truncate("A Very Long Nintendo Switch Game Title [0100ABC].nsp", 20)
	if len([]rune(got)) != 20 {
		t.Errorf("truncate produced %d runes, want 20: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, ".nsp") {
		t.Errorf("truncate should keep the extension, got %q", got)
	}
	if short := truncate("short.nsp", 20); short != "short.nsp" {
		t.Errorf("truncate should leave short strings alone, got %q", short)
	}
}
