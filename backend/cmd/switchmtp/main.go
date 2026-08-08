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

// Command switchmtp is a command-line client for a Nintendo Switch running
// DBI's MTP responder.
//
// It links the nxmtp engine directly rather than going through the C ABI the
// macOS app uses, which makes it the portable half of SwitchMTP: it builds and
// runs anywhere Go and libusb do.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=1.2.3"
var version = "dev"

// Exit codes. Anything a script might want to branch on gets its own code;
// everything else is 1.
const (
	exitOK          = 0
	exitError       = 1
	exitUsage       = 2
	exitNoDevice    = 3
	exitCancelled   = 4
	exitDeviceBusy  = 5
	exitUnsupported = 6
)

// options are the global flags, shared by every command.
type options struct {
	deviceID string
	json     bool
	verbose  bool
	yes      bool
	quiet    bool
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// parseInterspersed parses global flags wherever they appear on the command
// line, and returns the arguments that are not flags.
//
// Go's flag package stops parsing at the first non-flag argument, so it would
// read `switchmtp rm sdcard:/game.nsp --yes` as a request to delete two paths,
// one of them called "--yes". That is the natural way to type the command and
// the resulting error ("--yes" is not a device path) blames the user for the
// parser's limitation. Parsing after each positional argument accepts both
// orderings.
//
// Everything after a literal "--" is returned verbatim, so files whose names
// begin with a dash stay reachable.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var literal []string
	for i, arg := range args {
		if arg == "--" {
			args, literal = args[:i], args[i+1:]
			break
		}
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	var positional []string
	for {
		rest := fs.Args()
		if len(rest) == 0 {
			return append(positional, literal...), nil
		}
		positional = append(positional, rest[0])
		if err := fs.Parse(rest[1:]); err != nil {
			return nil, err
		}
	}
}

func run(args []string) int {
	var opts options
	fs := flag.NewFlagSet("switchmtp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&opts.deviceID, "device", "", "device id to use (see `switchmtp devices`); defaults to the only usable device")
	fs.BoolVar(&opts.json, "json", false, "emit machine-readable JSON instead of tables")
	fs.BoolVar(&opts.verbose, "verbose", false, "log MTP traffic to stderr")
	fs.BoolVar(&opts.yes, "yes", false, "do not ask for confirmation before destructive operations")
	fs.BoolVar(&opts.quiet, "quiet", false, "suppress progress output")
	fs.Usage = func() { usage(os.Stderr) }

	positional, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(os.Stdout)
			return exitOK
		}
		return exitUsage
	}

	rest := positional
	if len(rest) == 0 {
		usage(os.Stderr)
		return exitUsage
	}

	nxmtp.SetVerbose(opts.verbose)

	cmd, cmdArgs := rest[0], rest[1:]

	switch cmd {
	case "help", "-h", "--help":
		usage(os.Stdout)
		return exitOK
	case "version":
		err = cmdVersion(opts)
	case "devices":
		err = cmdDevices(opts, cmdArgs)
	case "info":
		err = cmdInfo(opts, cmdArgs)
	case "storages":
		err = cmdStorages(opts, cmdArgs)
	case "ls":
		err = cmdLs(opts, cmdArgs)
	case "get", "download":
		err = cmdGet(opts, cmdArgs)
	case "put", "upload":
		err = cmdPut(opts, cmdArgs)
	case "install":
		err = cmdInstall(opts, cmdArgs)
	case "mkdir":
		err = cmdMkdir(opts, cmdArgs)
	case "rm":
		err = cmdRm(opts, cmdArgs)
	case "mv":
		err = cmdMv(opts, cmdArgs)
	case "doctor":
		err = cmdDoctor(opts, cmdArgs)
	default:
		fmt.Fprintf(os.Stderr, "switchmtp: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return exitUsage
	}

	if err != nil {
		return report(err)
	}
	return exitOK
}

// usageError marks a mistake in how the command was invoked, as opposed to a
// failure while running it. It exits 2 and prints the command's own help.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error {
	return &usageError{msg: fmt.Sprintf(format, a...)}
}

// report prints an error in the most useful form available and maps it to an
// exit code.
//
// nxmtp.Error carries a Kind and often a Hint written for exactly this
// situation (DBI quirks, permission problems); surfacing the Hint verbatim is
// the whole reason those exist.
func report(err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(os.Stderr, "switchmtp: %s\n", ue.msg)
		return exitUsage
	}

	var ne *nxmtp.Error
	if errors.As(err, &ne) {
		fmt.Fprintf(os.Stderr, "switchmtp: %s\n", ne.Error())
		if ne.Hint != "" {
			fmt.Fprintf(os.Stderr, "\n%s\n", ne.Hint)
		}
		switch ne.Kind {
		case nxmtp.KindNoDevice:
			return exitNoDevice
		case nxmtp.KindCancelled:
			return exitCancelled
		case nxmtp.KindDeviceBusy:
			return exitDeviceBusy
		case nxmtp.KindUnsupported:
			return exitUnsupported
		case nxmtp.KindInvalidInput:
			return exitUsage
		}
		return exitError
	}

	fmt.Fprintf(os.Stderr, "switchmtp: %v\n", err)
	return exitError
}

// openClient opens the device the options select.
//
// It is a variable rather than a plain function so that tests can substitute a
// client backed by backend/fake. That is what lets every command below be
// exercised end to end on a machine with no Switch attached — including Linux
// CI, which is the whole point of this binary existing.
var openClient = func(opts options) (*nxmtp.Client, error) {
	id, err := selectDeviceID(opts)
	if err != nil {
		return nil, err
	}
	return nxmtp.Open(id)
}

// withDevice opens the selected device, runs fn, and always closes the
// session.
//
// Closing matters more here than it looks: the Switch holds the MTP session
// open, and leaving it dangling makes the next connection — from this tool or
// from the app — fail with a busy error.
func withDevice(opts options, fn func(*nxmtp.Client) error) error {
	client, err := openClient(opts)
	if err != nil {
		return err
	}
	defer client.Close()

	// Ctrl-C should cancel the in-flight MTP operation rather than kill the
	// process, so the session is closed cleanly and the Switch is not left
	// mid-transaction. A second Ctrl-C gives up and exits.
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	defer func() {
		signal.Stop(sig)
		close(done)
	}()
	go func() {
		select {
		case <-sig:
			fmt.Fprintln(os.Stderr, "\ncancelling… (press Ctrl-C again to force quit)")
			client.Cancel()
			select {
			case <-sig:
				fmt.Fprintln(os.Stderr, "forced quit")
				os.Exit(exitCancelled)
			case <-done:
			}
		case <-done:
		}
	}()

	return fn(client)
}

// selectDeviceID resolves the --device flag, or picks the device when there is
// no ambiguity.
func selectDeviceID(opts options) (string, error) {
	if opts.deviceID != "" {
		return opts.deviceID, nil
	}

	refs, err := nxmtp.FindDevices()
	if err != nil {
		return "", err
	}

	var usable []nxmtp.DeviceRef
	for _, r := range refs {
		if r.Usable {
			usable = append(usable, r)
		}
	}

	switch len(usable) {
	case 1:
		return usable[0].ID(), nil
	case 0:
		return "", noUsableDeviceError(refs)
	default:
		var b strings.Builder
		b.WriteString("several usable devices are connected; choose one with --device:")
		for _, r := range usable {
			fmt.Fprintf(&b, "\n  %s  %s", r.ID(), r.DisplayName)
		}
		return "", &nxmtp.Error{Kind: nxmtp.KindInvalidInput, Op: "selectDevice", Msg: b.String()}
	}
}

// noUsableDeviceError explains *why* there is nothing to talk to, which is
// almost always more useful than the fact itself: a Switch in RCM or in
// Homebrew Menu enumerates over USB but cannot speak MTP, and saying so saves
// a support round trip.
func noUsableDeviceError(refs []nxmtp.DeviceRef) error {
	for _, r := range refs {
		if r.Advice != "" {
			return &nxmtp.Error{
				Kind: nxmtp.KindNoDevice,
				Op:   "selectDevice",
				Msg:  "found " + r.DisplayName + ", but it cannot be used right now",
				Hint: r.Advice,
			}
		}
	}
	return &nxmtp.Error{
		Kind: nxmtp.KindNoDevice,
		Op:   "selectDevice",
		Msg:  "no Nintendo Switch running an MTP responder was found",
		Hint: "Open DBI on the console and choose \"Run MTP responder\", then reconnect the USB cable.\nRun `switchmtp doctor` for a full diagnosis.",
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `switchmtp — command-line client for a Nintendo Switch running DBI's MTP responder

Usage:
  switchmtp [flags] <command> [arguments]

Commands:
  devices              List connected devices
  info                 Show details and capabilities of the selected device
  storages             List the storages the device exposes
  ls <path>            List a directory
  get <path>... <dir>  Copy from the device to the local disk
  put <file>... <path> Copy from the local disk to the device
  install <file>...    Install NSP/NSZ/XCI/XCZ files, one after another
  mkdir <path>         Create a directory
  rm <path>...         Delete files or directories
  mv <path> <name>     Rename a file or directory
  doctor               Diagnose connection problems
  version              Print the version

Device paths are written <storage>:<path>, where <storage> is a storage id, a
name such as "sdcard", or a prefix of the storage's display name:

  switchmtp ls sdcard:/
  switchmtp ls 65537:/switch
  switchmtp get sdcard:/switch/config.ini ./
  switchmtp install ~/Downloads/*.nsp

Flags:
  --device <id>  Device to use; defaults to the only usable one
  --json         Emit JSON instead of tables
  --verbose      Log MTP traffic to stderr
  --quiet        Suppress progress output
  --yes          Skip confirmation prompts

Only one program can hold the USB device at a time: close the SwitchMTP app
before using this tool, and vice versa.
`)
}
