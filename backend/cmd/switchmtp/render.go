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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// humanBytes formats a byte count the way a file manager would.
//
// Decimal units, not binary: the Switch, DBI and every SD card vendor use GB
// to mean 10^9, and disagreeing with the number printed on the console would
// be worse than being technically pedantic.
func humanBytes(n int64) string {
	if n < 0 {
		return "—"
	}
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := [...]string{"kB", "MB", "GB", "TB", "PB"}[exp]
	if value < 10 {
		return fmt.Sprintf("%.2f %s", value, suffix)
	}
	if value < 100 {
		return fmt.Sprintf("%.1f %s", value, suffix)
	}
	return fmt.Sprintf("%.0f %s", value, suffix)
}

// humanDuration formats an elapsed or remaining time compactly.
func humanDuration(d time.Duration) string {
	if d < 0 {
		return "—"
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// humanRate formats a transfer rate. Bytes per second in, human-readable out.
func humanRate(bytesPerSecond float64) string {
	if bytesPerSecond <= 0 {
		return "—"
	}
	return humanBytes(int64(bytesPerSecond)) + "/s"
}

// table accumulates rows and prints them with aligned columns.
//
// This exists instead of text/tabwriter because the columns need individually
// chosen alignment (sizes right, names left) and tabwriter aligns everything
// the same way.
type table struct {
	headers []string
	rows    [][]string
	// right marks column indices that are right-aligned.
	right map[int]bool
}

func newTable(headers ...string) *table {
	return &table{headers: headers, right: map[int]bool{}}
}

// rightAlign marks columns as right-aligned. Use for numbers.
func (t *table) rightAlign(cols ...int) *table {
	for _, c := range cols {
		t.right[c] = true
	}
	return t
}

func (t *table) add(cells ...string) {
	t.rows = append(t.rows, cells)
}

func (t *table) render(w io.Writer) {
	if len(t.rows) == 0 {
		return
	}
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = displayWidth(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if i < len(widths) && displayWidth(c) > widths[i] {
				widths[i] = displayWidth(c)
			}
		}
	}

	writeRow := func(cells []string) {
		var b strings.Builder
		for i, c := range cells {
			if i >= len(widths) {
				break
			}
			pad := widths[i] - displayWidth(c)
			if pad < 0 {
				pad = 0
			}
			last := i == len(widths)-1
			switch {
			case t.right[i]:
				b.WriteString(strings.Repeat(" ", pad))
				b.WriteString(c)
			case last:
				// Never pad the final column: trailing whitespace makes
				// copy-pasted output messy and shell-unfriendly.
				b.WriteString(c)
			default:
				b.WriteString(c)
				b.WriteString(strings.Repeat(" ", pad))
			}
			if !last {
				b.WriteString("  ")
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}

	writeRow(t.headers)
	sep := make([]string, len(t.headers))
	for i := range sep {
		sep[i] = strings.Repeat("─", widths[i])
	}
	writeRow(sep)
	for _, r := range t.rows {
		writeRow(r)
	}
}

// displayWidth counts runes rather than bytes, so that non-ASCII game titles
// — which on a Switch are common — do not skew column alignment.
func displayWidth(s string) int {
	return len([]rune(s))
}

// emitJSON writes a value as indented JSON to stdout.
func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// truncate shortens a string for single-line display, keeping the end (which
// for filenames carries the extension and the distinguishing part).
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max || max < 4 {
		return s
	}
	return "…" + string(r[len(r)-(max-1):])
}
