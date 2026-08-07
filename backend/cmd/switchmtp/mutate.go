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
	"bufio"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

func cmdMkdir(opts options, args []string) error {
	if len(args) != 1 {
		return usagef("mkdir needs exactly one device path, for example `switchmtp mkdir sdcard:/switch/mytool`")
	}
	rp, err := parseRemotePath(args[0])
	if err != nil {
		return err
	}
	if rp.Path == "/" {
		return usagef("mkdir needs a path inside the storage, not the storage root")
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		st, err := storageFor(c, rp.Selector)
		if err != nil {
			return err
		}
		if err := c.MakeDirectory(st.Sid, rp.Path); err != nil {
			return err
		}
		if !opts.quiet {
			fmt.Printf("Created %s on %s.\n", rp.Path, st.DisplayName)
		}
		return nil
	})
}

func cmdRm(opts options, args []string) error {
	if len(args) == 0 {
		return usagef("rm needs at least one device path")
	}

	var selector string
	var paths []string
	for _, a := range args {
		rp, err := parseRemotePath(a)
		if err != nil {
			return err
		}
		if rp.Path == "/" {
			return usagef("refusing to delete the root of a storage")
		}
		if selector == "" {
			selector = rp.Selector
		} else if !strings.EqualFold(selector, rp.Selector) {
			return usagef("all paths must be on the same storage (%q and %q are not)", selector, rp.Selector)
		}
		paths = append(paths, rp.Path)
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		st, err := storageFor(c, selector)
		if err != nil {
			return err
		}
		if !st.Capabilities.Delete {
			return &nxmtp.Error{
				Kind: nxmtp.KindReadOnly,
				Op:   "rm",
				Msg:  "storage \"" + st.DisplayName + "\" does not allow deleting",
			}
		}

		// Deletion recurses into directories and there is no undo on a
		// console, so confirm unless the caller opted out.
		if !opts.yes {
			ok, err := confirm(fmt.Sprintf("Delete %s from %s? Directories are removed with their contents.",
				plural(len(paths), "item", "items"), st.DisplayName))
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		if err := c.Delete(st.Sid, paths); err != nil {
			return err
		}
		if !opts.quiet {
			fmt.Printf("Deleted %s.\n", plural(len(paths), "item", "items"))
		}
		return nil
	})
}

func cmdMv(opts options, args []string) error {
	if len(args) != 2 {
		return usagef("mv needs a device path and a new name, for example `switchmtp mv sdcard:/switch/old.nro new.nro`")
	}
	rp, err := parseRemotePath(args[0])
	if err != nil {
		return err
	}
	newName := strings.TrimSpace(args[1])

	// Accept a full device path as the second argument as long as it only
	// renames: moving between directories is a different MTP operation and
	// silently doing the wrong one would be worse than refusing.
	if isRemotePath(newName) {
		target, err := parseRemotePath(newName)
		if err != nil {
			return err
		}
		if !strings.EqualFold(target.Selector, rp.Selector) || path.Dir(target.Path) != path.Dir(rp.Path) {
			return usagef("mv can rename within a directory but cannot move between directories; use get and put instead")
		}
		newName = path.Base(target.Path)
	}

	if newName == "" || strings.ContainsAny(newName, "/\\") {
		return usagef("%q is not a valid name", args[1])
	}

	return withDevice(opts, func(c *nxmtp.Client) error {
		st, err := storageFor(c, rp.Selector)
		if err != nil {
			return err
		}
		if err := c.Rename(st.Sid, rp.Path, newName); err != nil {
			return err
		}
		if !opts.quiet {
			fmt.Printf("Renamed %s to %s.\n", path.Base(rp.Path), newName)
		}
		return nil
	})
}

// confirm asks a yes/no question on the terminal.
//
// With no terminal attached there is nobody to answer, so it refuses rather
// than defaulting to yes — a piped script that meant to delete things can say
// so with --yes.
func confirm(question string) (bool, error) {
	if !isTerminal(os.Stdin) {
		return false, fmt.Errorf("%s\nRefusing to continue without a terminal; pass --yes to confirm non-interactively", question)
	}
	fmt.Printf("%s [y/N] ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
