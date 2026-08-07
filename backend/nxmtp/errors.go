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
	"errors"
	"fmt"
	"strings"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// ErrorKind classifies a failure so the UI can present something actionable
// instead of a raw MTP response code.
type ErrorKind string

const (
	KindUnknown       ErrorKind = "unknown"
	KindNoDevice      ErrorKind = "noDevice"
	KindDeviceBusy    ErrorKind = "deviceBusy"
	KindDisconnected  ErrorKind = "disconnected"
	KindUnsupported   ErrorKind = "operationUnsupported"
	KindReadOnly      ErrorKind = "readOnly"
	KindWriteOnly     ErrorKind = "writeOnly"
	KindStorageFull   ErrorKind = "storageFull"
	KindAccessDenied  ErrorKind = "accessDenied"
	KindNotFound      ErrorKind = "notFound"
	KindAlreadyExists ErrorKind = "alreadyExists"
	KindCancelled     ErrorKind = "cancelled"
	KindInvalidInput  ErrorKind = "invalidInput"
	KindWrongMode     ErrorKind = "wrongDeviceMode"
	KindLocalIO       ErrorKind = "localIO"
)

// Error is the error type returned throughout nxmtp. Kind drives the UI's
// choice of message and remediation; Hint carries DBI/macOS-specific advice
// that the UI can surface verbatim.
type Error struct {
	Kind ErrorKind
	Op   string
	Msg  string
	Hint string
	Err  error
}

func (e *Error) Error() string {
	var b string
	if e.Op != "" {
		b = e.Op + ": "
	}
	if e.Msg != "" {
		b += e.Msg
	} else if e.Err != nil {
		b += e.Err.Error()
	}
	if e.Err != nil && e.Msg != "" {
		b += ": " + e.Err.Error()
	}
	return b
}

func (e *Error) Unwrap() error { return e.Err }

func newErr(kind ErrorKind, op, msg string) *Error {
	return &Error{Kind: kind, Op: op, Msg: msg}
}

func wrapErr(kind ErrorKind, op string, err error) *Error {
	return &Error{Kind: kind, Op: op, Err: err}
}

// KindOf extracts the ErrorKind from an error, classifying raw MTP response
// codes on the way. Anything unrecognised becomes KindUnknown.
func KindOf(err error) ErrorKind {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	var rc mtp.RCError
	if errors.As(err, &rc) {
		return kindOfRC(uint16(rc))
	}
	return KindUnknown
}

func kindOfRC(rc uint16) ErrorKind {
	switch rc {
	case mtp.RC_OperationNotSupported,
		// DBI advertises GetObjectPropList but rejects the parameter
		// combination we send, answering ParameterNotSupported rather than
		// OperationNotSupported. Semantically that is still "you cannot use
		// this call", so it has to demote the capability too -- otherwise the
		// fast path is retried forever and every listing fails outright.
		mtp.RC_ParameterNotSupported,
		mtp.RC_InvalidParameter,
		mtp.RC_MTP_Invalid_ObjectPropCode,
		mtp.RC_MTP_Specification_By_Group_Unsupported,
		mtp.RC_MTP_Specification_By_Depth_Unsupported,
		mtp.RC_MTP_ObjectProp_Not_Supported:
		return KindUnsupported
	case mtp.RC_StoreReadOnly, mtp.RC_ObjectWriteProtected, mtp.RC_StoreNotAvailable:
		return KindReadOnly
	case mtp.RC_StoreFull:
		return KindStorageFull
	case mtp.RC_AccessDenied:
		return KindAccessDenied
	case mtp.RC_InvalidObjectHandle, mtp.RC_InvalidStorageId, mtp.RC_InvalidParentObject:
		return KindNotFound
	case mtp.RC_DeviceBusy:
		return KindDeviceBusy
	case mtp.RC_SessionNotOpen, mtp.RC_InvalidTransactionID:
		return KindDisconnected
	default:
		return KindUnknown
	}
}

// IsUnsupported reports whether err means "the device does not implement this
// operation". This is the check that lets every optional MTP call degrade
// instead of aborting a listing.
func IsUnsupported(err error) bool {
	return KindOf(err) == KindUnsupported
}

// IsDisconnected reports whether the device went away mid-operation. libusb
// surfaces this as a plain error string rather than an MTP response code, so we
// have to pattern-match as well as check the response code.
func IsDisconnected(err error) bool {
	if err == nil {
		return false
	}
	if KindOf(err) == KindDisconnected {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{"no such device", "no_device", "device not found", "not_found", "device has been disconnected"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// classify converts any error from the transport into a *Error, attaching a
// remediation hint where we have one.
func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	kind := KindOf(err)
	if kind == KindUnknown && IsDisconnected(err) {
		kind = KindDisconnected
	}
	out := &Error{Kind: kind, Op: op, Err: err}
	switch kind {
	case KindDisconnected:
		out.Hint = "The Switch disconnected. Check the USB cable and make sure DBI's MTP responder is still running."
	case KindDeviceBusy:
		out.Hint = "The device is busy. If an installation is running on the Switch, wait for it to finish."
	case KindReadOnly:
		out.Hint = "This storage is read-only."
	case KindStorageFull:
		out.Hint = "The storage is full."
	}
	return out
}

// rcOf returns the MTP response code carried by err, if any.
func rcOf(err error) (uint16, bool) {
	var rc mtp.RCError
	if errors.As(err, &rc) {
		return uint16(rc), true
	}
	return 0, false
}

func errf(kind ErrorKind, op, format string, args ...interface{}) *Error {
	return &Error{Kind: kind, Op: op, Msg: fmt.Sprintf(format, args...)}
}
