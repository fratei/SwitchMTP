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
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

func cmdVersion(opts options) error {
	if opts.json {
		return emitJSON(map[string]string{
			"version": version,
			"go":      runtime.Version(),
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
		})
	}
	fmt.Printf("switchmtp %s (%s, %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return nil
}

func cmdDevices(opts options, args []string) error {
	if len(args) > 0 {
		return usagef("devices takes no arguments")
	}

	refs, err := nxmtp.FindDevices()
	if err != nil {
		return err
	}

	if opts.json {
		if refs == nil {
			refs = []nxmtp.DeviceRef{}
		}
		return emitJSON(refs)
	}

	if len(refs) == 0 {
		fmt.Println("No Nintendo devices found.")
		fmt.Println("\nRun `switchmtp doctor` to find out why.")
		return nil
	}

	t := newTable("ID", "DEVICE", "STATE")
	for _, r := range refs {
		state := "ready"
		if !r.Usable {
			state = "unusable"
		}
		t.add(r.ID(), r.DisplayName, state)
	}
	t.render(os.Stdout)

	// Advice is per-device and often multi-line, so it goes below the table
	// rather than into a column.
	for _, r := range refs {
		if !r.Usable && r.Advice != "" {
			fmt.Printf("\n%s:\n  %s\n", r.DisplayName, strings.ReplaceAll(r.Advice, "\n", "\n  "))
		}
	}
	return nil
}

func cmdInfo(opts options, args []string) error {
	if len(args) > 0 {
		return usagef("info takes no arguments")
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		d := c.Details()
		if opts.json {
			return emitJSON(d)
		}

		fmt.Printf("%s\n", d.DisplayName)
		fmt.Printf("  Device id      %s\n", d.DeviceID)
		fmt.Printf("  Profile        %s\n", d.DeviceProfile)
		if mi := d.MTPDeviceInfo; mi != nil {
			fmt.Printf("  Manufacturer   %s\n", strings.TrimSpace(mi.Manufacturer))
			fmt.Printf("  Model          %s\n", strings.TrimSpace(mi.Model))
			fmt.Printf("  Version        %s\n", strings.TrimSpace(mi.DeviceVersion))
			fmt.Printf("  Serial         %s\n", strings.TrimSpace(mi.SerialNumber))
			fmt.Printf("  MTP version    %d\n", mi.StandardVersion)
		}
		if d.Advice != "" {
			fmt.Printf("\n%s\n", d.Advice)
		}

		if caps := d.Capabilities; caps != nil {
			fmt.Println("\nOptional MTP operations:")
			// Sorted so the output is stable and diffable between runs and
			// between devices.
			for _, row := range capabilityRows(caps) {
				mark := "no"
				if row.supported {
					mark = "yes"
				}
				fmt.Printf("  %-24s %s\n", row.name, mark)
			}
			fmt.Printf("\nDerived actions: rename=%s delete=%s move=%s\n",
				yesNo(caps.CanRename), yesNo(caps.CanDelete), yesNo(caps.CanMove))
		}
		return nil
	})
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

type capabilityRow struct {
	name      string
	supported bool
}

// capabilityRows flattens Capabilities for display. It is written out by hand
// rather than by reflection so that a new capability field shows up as a
// compile-time reminder to describe it, not as a silently missing row.
func capabilityRows(c *nxmtp.Capabilities) []capabilityRow {
	rows := []capabilityRow{
		{"AndroidExtension", c.AndroidExtension},
		{"AndroidPartialTransfer", c.AndroidPartialTransfer},
		{"CopyObject", c.CopyObject},
		{"DeleteObject", c.DeleteObject},
		{"GetNumObjects", c.GetNumObjects},
		{"GetObjectPropList", c.GetObjectPropList},
		{"GetObjectPropValue", c.GetObjectPropValue},
		{"GetObjectPropsSupported", c.GetObjectPropsSupport},
		{"GetPartialObject", c.GetPartialObject},
		{"MoveObject", c.MoveObject},
		{"SendObject", c.SendObject},
		{"SetObjectPropValue", c.SetObjectPropValue},
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	return rows
}

func cmdStorages(opts options, args []string) error {
	if len(args) > 0 {
		return usagef("storages takes no arguments")
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		storages, err := c.Storages()
		if err != nil {
			return err
		}
		if opts.json {
			if storages == nil {
				storages = []nxmtp.Storage{}
			}
			return emitJSON(storages)
		}

		t := newTable("ID", "NAME", "KIND", "ACCESS", "FREE", "TOTAL").rightAlign(0, 4, 5)
		for _, s := range storages {
			free, total := "—", "—"
			if s.SizeReliable {
				free = humanBytes(int64(s.Info.FreeSpaceInBytes))
				total = humanBytes(int64(s.Info.MaxCapability))
			}
			t.add(
				strconv.FormatUint(uint64(s.Sid), 10),
				s.DisplayName,
				string(s.Kind),
				accessLabel(s),
				free,
				total,
			)
		}
		t.render(os.Stdout)

		for _, s := range storages {
			if s.Description != "" && s.Description != s.DisplayName {
				fmt.Printf("\n%s: %s\n", s.DisplayName, s.Description)
			}
		}
		return nil
	})
}

// accessLabel summarises a storage's capabilities in one column.
//
// "drop-only" is the interesting case and the reason SwitchMTP exists: DBI's
// install storages accept writes but cannot be listed, which no generic MTP
// client models.
func accessLabel(s nxmtp.Storage) string {
	switch {
	case s.Capabilities.Write && !s.Capabilities.Browse:
		return "drop-only"
	case s.Capabilities.Write:
		return "read/write"
	case s.Capabilities.Browse:
		return "read-only"
	default:
		return "none"
	}
}

func cmdLs(opts options, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	recursive := fs.Bool("R", false, "list subdirectories recursively")
	all := fs.Bool("a", false, "include hidden files and filesystem artefacts")
	if err := fs.Parse(args); err != nil {
		return usagef("ls: %v", err)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return usagef("ls needs exactly one device path, for example `switchmtp ls sdcard:/`")
	}

	rp, err := parseRemotePath(rest[0])
	if err != nil {
		return err
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		st, err := storageFor(c, rp.Selector)
		if err != nil {
			return err
		}

		files, err := c.Walk(nxmtp.WalkOptions{
			StorageID:           st.Sid,
			FullPath:            rp.Path,
			Recursive:           *recursive,
			SkipHiddenFiles:     !*all,
			SkipDisallowedFiles: !*all,
		})
		if err != nil {
			return err
		}

		if opts.json {
			if files == nil {
				files = []nxmtp.FileInfo{}
			}
			return emitJSON(files)
		}

		if len(files) == 0 {
			fmt.Printf("%s is empty.\n", rp.Path)
			return nil
		}

		// Directories first, then alphabetically — the same ordering the app's
		// file list uses by default, so the two agree.
		sort.SliceStable(files, func(i, j int) bool {
			if files[i].IsFolder != files[j].IsFolder {
				return files[i].IsFolder
			}
			return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
		})

		nameColumn := "NAME"
		if *recursive {
			nameColumn = "PATH"
		}
		t := newTable("SIZE", "MODIFIED", nameColumn).rightAlign(0)
		var totalBytes int64
		var fileCount, dirCount int
		for _, f := range files {
			size := "<dir>"
			switch {
			case f.IsFolder:
				dirCount++
			case f.SizeUnknown:
				size = "?"
				fileCount++
			default:
				size = humanBytes(f.Size)
				totalBytes += f.Size
				fileCount++
			}
			modified := "—"
			if !f.DateAdded.IsZero() {
				modified = f.DateAdded.Local().Format("2006-01-02 15:04")
			}
			name := f.Name
			if *recursive {
				name = f.Path
			}
			t.add(size, modified, name)
		}
		t.render(os.Stdout)
		fmt.Printf("\n%s in %s, %s\n",
			plural(fileCount, "file", "files"),
			plural(dirCount, "directory", "directories"),
			humanBytes(totalBytes))
		return nil
	})
}

// storageFor resolves a selector against the connected device.
func storageFor(c *nxmtp.Client, selector string) (*nxmtp.Storage, error) {
	storages, err := c.Storages()
	if err != nil {
		return nil, err
	}
	st, err := resolveStorage(storages, selector)
	if err != nil {
		return nil, &nxmtp.Error{Kind: nxmtp.KindInvalidInput, Op: "resolveStorage", Msg: err.Error()}
	}
	return st, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
