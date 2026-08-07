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
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

// progressPrinter renders transfer progress to a terminal.
//
// Two output modes, chosen by whether stderr is a terminal:
//
//   - Interactive: one line, rewritten in place with a carriage return.
//   - Redirected: a new line only when something meaningful changes, because a
//     log file full of 10-per-second redraws is useless.
//
// It writes to stderr so that `--json` output on stdout stays parseable while
// a transfer is running.
type progressPrinter struct {
	mu sync.Mutex
	w  io.Writer
	// interactive selects in-place redraw over append-only lines.
	interactive bool
	quiet       bool

	lastLine string
	// lastNote tracks phase changes (e.g. entering the install phase) so the
	// non-interactive mode can log them exactly once.
	lastNote     string
	lastFile     string
	dirty        bool
	lastNonInter time.Time
}

func newProgressPrinter(quiet bool) *progressPrinter {
	return &progressPrinter{
		w:           os.Stderr,
		interactive: isTerminal(os.Stderr),
		quiet:       quiet,
	}
}

func (p *progressPrinter) update(pr nxmtp.Progress) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	line := formatProgress(pr)

	if p.interactive {
		// Pad to the previous width so shorter lines do not leave debris from
		// the longer line they replaced.
		pad := len([]rune(p.lastLine)) - len([]rune(line))
		if pad < 0 {
			pad = 0
		}
		fmt.Fprintf(p.w, "\r%s%s", line, strings.Repeat(" ", pad))
		p.lastLine = line
		p.dirty = true
		return
	}

	// Non-interactive: log on a phase change, a new file, or every few
	// seconds so a long transfer still shows signs of life in a log.
	changed := pr.Note != p.lastNote || pr.Name != p.lastFile
	if changed || time.Since(p.lastNonInter) >= 5*time.Second {
		fmt.Fprintln(p.w, line)
		p.lastNote = pr.Note
		p.lastFile = pr.Name
		p.lastNonInter = time.Now()
	}
}

// finish clears the in-place progress line so the next thing printed starts on
// a clean row.
func (p *progressPrinter) finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interactive && p.dirty {
		fmt.Fprintf(p.w, "\r%s\r", strings.Repeat(" ", len([]rune(p.lastLine))))
		p.dirty = false
		p.lastLine = ""
	}
}

// formatProgress renders one progress update as a single line.
//
// The indefinite case is not cosmetic. While DBI commits an install the byte
// count has already reached 100% but the console is still working, and showing
// a full bar there is exactly what makes the app look wedged — so an
// indefinite update deliberately shows the note instead of a percentage.
func formatProgress(pr nxmtp.Progress) string {
	var b strings.Builder

	switch pr.Status {
	case nxmtp.StatusPreprocessing:
		b.WriteString("scanning  ")
	case nxmtp.StatusInstalling:
		b.WriteString("installing")
	case nxmtp.StatusCancelled:
		b.WriteString("cancelled ")
	case nxmtp.StatusFailed:
		b.WriteString("failed    ")
	case nxmtp.StatusCompleted:
		b.WriteString("done      ")
	default:
		b.WriteString("copying   ")
	}
	b.WriteString("  ")

	if pr.TotalFiles > 1 {
		n := pr.CurrentFile
		if n == 0 {
			n = pr.FilesSent + 1
		}
		if n > pr.TotalFiles {
			n = pr.TotalFiles
		}
		fmt.Fprintf(&b, "[%d/%d] ", n, pr.TotalFiles)
	}

	if pr.Name != "" {
		b.WriteString(truncate(pr.Name, 44))
		b.WriteString("  ")
	}

	if pr.Indefinite {
		if pr.Note != "" {
			b.WriteString(pr.Note)
		} else {
			b.WriteString("working…")
		}
		return b.String()
	}

	if pr.ActiveFileSize.Total > 0 {
		fmt.Fprintf(&b, "%s %5.1f%%  %s/%s",
			bar(pr.ActiveFileSize.Progress),
			pr.ActiveFileSize.Progress,
			humanBytes(pr.ActiveFileSize.Sent),
			humanBytes(pr.ActiveFileSize.Total))
	}
	if pr.Speed > 0 {
		fmt.Fprintf(&b, "  %s", humanRate(pr.Speed))
		// Only worth showing once there is at least a second left; "ETA 0s"
		// on a nearly finished file is noise.
		if eta := estimate(pr); eta >= time.Second {
			fmt.Fprintf(&b, "  ETA %s", humanDuration(eta))
		}
	}
	return b.String()
}

// estimate projects a remaining time from the *bulk* counters, so that a
// multi-file transfer gives one honest total rather than restarting the
// estimate at every file.
func estimate(pr nxmtp.Progress) time.Duration {
	if pr.Speed <= 0 {
		return 0
	}
	remaining := pr.BulkFileSize.Total - pr.BulkFileSize.Sent
	if remaining <= 0 {
		return 0
	}
	// Scale to nanoseconds before converting: doing the conversion first
	// truncates the ratio to whole seconds and loses everything below a
	// minute of accuracy.
	return time.Duration(float64(remaining) / pr.Speed * float64(time.Second))
}

func bar(percent float64) string {
	const width = 20
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent / 100 * width)
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// isTerminal reports whether a file is attached to a character device, which
// is the portable-enough proxy for "a terminal" without adding a dependency
// just to draw a progress bar.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func cmdGet(opts options, args []string) error {
	if len(args) < 2 {
		return usagef("get needs at least one device path and a local destination, for example `switchmtp get sdcard:/switch/config.ini ./`")
	}

	sources := args[:len(args)-1]
	dest := args[len(args)-1]

	if isRemotePath(dest) {
		return usagef("the last argument to get is the local destination, but %q looks like a device path", dest)
	}

	var selector string
	var paths []string
	for _, s := range sources {
		rp, err := parseRemotePath(s)
		if err != nil {
			return err
		}
		// One transfer runs against one storage; mixing them would need
		// several sessions and makes the progress totals meaningless.
		if selector == "" {
			selector = rp.Selector
		} else if !strings.EqualFold(selector, rp.Selector) {
			return usagef("all sources must be on the same storage (%q and %q are not)", selector, rp.Selector)
		}
		paths = append(paths, rp.Path)
	}

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("cannot create destination %q: %w", dest, err)
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		st, err := storageFor(c, selector)
		if err != nil {
			return err
		}
		if !st.Capabilities.Read {
			return &nxmtp.Error{
				Kind: nxmtp.KindWriteOnly,
				Op:   "get",
				Msg:  "storage \"" + st.DisplayName + "\" cannot be read",
				Hint: "DBI's install storages accept files but cannot be listed or read back.",
			}
		}

		p := newProgressPrinter(opts.quiet)
		summary, err := c.Download(
			nxmtp.DownloadRequest{
				StorageID:       st.Sid,
				Sources:         paths,
				Destination:     dest,
				PreprocessFiles: true,
			},
			nil,
			p.update,
		)
		p.finish()
		if err != nil {
			return err
		}
		return reportSummary(opts, "Downloaded", summary)
	})
}

func cmdPut(opts options, args []string) error {
	if len(args) < 2 {
		return usagef("put needs at least one local file and a device destination, for example `switchmtp put game.nsp sdcard:/games`")
	}

	sources := args[:len(args)-1]
	dest := args[len(args)-1]

	rp, err := parseRemotePath(dest)
	if err != nil {
		return err
	}
	for _, s := range sources {
		if _, err := os.Stat(s); err != nil {
			return fmt.Errorf("cannot read %q: %w", s, err)
		}
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		st, err := storageFor(c, rp.Selector)
		if err != nil {
			return err
		}

		p := newProgressPrinter(opts.quiet)
		summary, err := c.Upload(
			nxmtp.UploadRequest{
				StorageID:       st.Sid,
				Sources:         sources,
				Destination:     rp.Path,
				PreprocessFiles: true,
			},
			nil,
			p.update,
		)
		p.finish()
		if err != nil {
			return err
		}
		return reportSummary(opts, "Uploaded", summary)
	})
}

// cmdInstall sends installable files to one of DBI's install storages.
//
// It is a separate command from put even though both call Upload, because
// choosing the storage is the entire difficulty: install storages are
// write-only and are not something a user can sensibly discover by browsing.
// nxmtp serialises the titles itself, so this stays a single call — the
// console installs one game at a time regardless of how many are queued.
func cmdInstall(opts options, args []string) error {
	target := nxmtp.KindSDInstall
	label := "SD card"
	var files []string
	for _, a := range args {
		switch a {
		case "--nand", "-nand":
			target = nxmtp.KindNandInstall
			label = "system memory"
		case "--sd", "-sd":
			target = nxmtp.KindSDInstall
			label = "SD card"
		default:
			files = append(files, a)
		}
	}

	if len(files) == 0 {
		return usagef("install needs at least one NSP, NSZ, XCI or XCZ file")
	}

	var rejected []string
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("cannot read %q: %w", f, err)
		}
		if !nxmtp.IsInstallable(f) {
			rejected = append(rejected, f)
		}
	}
	// Check locally before opening the device: telling the user their file is
	// the wrong type should not require a Switch to be plugged in.
	if len(rejected) > 0 {
		return usagef("these files cannot be installed: %s\nInstall storages accept %s only.",
			strings.Join(rejected, ", "), strings.Join(nxmtp.InstallableExtensions, ", "))
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		storages, err := c.Storages()
		if err != nil {
			return err
		}
		var st *nxmtp.Storage
		for i := range storages {
			if storages[i].Kind == target {
				st = &storages[i]
				break
			}
		}
		if st == nil {
			return &nxmtp.Error{
				Kind: nxmtp.KindNotFound,
				Op:   "install",
				Msg:  "this device does not expose an install target for " + label,
				Hint: "DBI shows install storages only while its MTP responder is running. Check that the SD card is inserted and that DBI is up to date.",
			}
		}

		if !opts.quiet {
			fmt.Fprintf(os.Stderr, "Installing %s to %s via %s.\n",
				plural(len(files), "file", "files"), label, st.DisplayName)
			fmt.Fprintln(os.Stderr, "Watch the console for the install progress DBI reports itself.")
		}

		p := newProgressPrinter(opts.quiet)
		summary, err := c.Upload(
			nxmtp.UploadRequest{
				StorageID:       st.Sid,
				Sources:         files,
				Destination:     "/",
				PreprocessFiles: true,
			},
			nil,
			p.update,
		)
		p.finish()
		if err != nil {
			return err
		}
		return reportSummary(opts, "Installed", summary)
	})
}

func reportSummary(opts options, verb string, s *nxmtp.TransferSummary) error {
	if s == nil {
		return nil
	}
	if opts.json {
		return emitJSON(s)
	}
	fmt.Printf("%s %s (%s) in %s.\n",
		verb,
		plural(int(s.TotalFiles), "file", "files"),
		humanBytes(s.TotalBytes),
		humanDuration(time.Duration(s.Elapsed*float64(time.Second))))
	if s.Note != "" {
		fmt.Println(s.Note)
	}
	if len(s.Skipped) > 0 {
		fmt.Printf("Skipped %s: %s\n",
			plural(len(s.Skipped), "file", "files"),
			strings.Join(s.Skipped, ", "))
	}
	return nil
}
