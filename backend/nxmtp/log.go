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

import (
	"io"
	"log"
	"os"
	"sync/atomic"
)

// Verbose enables protocol-level logging. It is off by default and turned on
// by the SWITCHMTP_DEBUG environment variable or by the CLI's --verbose flag.
var verbose atomic.Bool

func init() {
	if os.Getenv("SWITCHMTP_DEBUG") != "" {
		verbose.Store(true)
	}
	log.SetOutput(gatedWriter{})
	log.SetFlags(log.LstdFlags)
}

// gatedWriter drops log output unless verbose logging is on.
//
// The vendored go-mtpfs/mtp package logs to the standard logger
// unconditionally -- notably "fatal error LIBUSB_ERROR_NOT_FOUND; closing
// connection" once per non-MTP USB device on every scan. Since the app polls
// for devices, that would flood the user's system log with messages that are
// expected and harmless. Gating the global logger here keeps the vendored
// source unmodified while making the output opt-in.
type gatedWriter struct{}

func (gatedWriter) Write(p []byte) (int, error) {
	if verbose.Load() {
		return os.Stderr.Write(p)
	}
	return len(p), nil
}

// SetVerbose toggles diagnostic logging.
func SetVerbose(v bool) { verbose.Store(v) }

// Verbose reports whether diagnostic logging is enabled.
func Verbose() bool { return verbose.Load() }

// LogWriter returns the destination for diagnostic output, for callers that
// need to hand an io.Writer to another package.
func LogWriter() io.Writer { return gatedWriter{} }

func logf(format string, args ...interface{}) {
	if verbose.Load() {
		log.Printf("[nxmtp] "+format, args...)
	}
}
