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

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// Transport is the narrow slice of the MTP protocol engine that nxmtp actually
// uses. Everything above this interface is pure logic and can therefore be
// exercised against the fake device in package fake, with no hardware and no USB
// stack involved.
//
// *mtp.Device satisfies this interface directly; see deviceTransport.
type Transport interface {
	// Session lifecycle.
	OpenSession() error
	CloseSession() error
	Close() error
	Done()

	// Identity.
	GetDeviceInfo(info *mtp.DeviceInfo) error
	GetUsbInfo() (*mtp.UsbDeviceInfo, error)
	ID() (string, error)

	// Storage.
	GetStorageIDs(info *mtp.Uint32Array) error
	GetStorageInfo(id uint32, info *mtp.StorageInfo) error

	// Object enumeration.
	GetObjectHandles(storageID, objFormatCode, parent uint32, info *mtp.Uint32Array) error
	GetObjectInfo(handle uint32, info *mtp.ObjectInfo) error
	GetNumObjects(storageID uint32, formatCode uint16, parent uint32) (uint32, error)

	// Object properties. These are optional in the MTP standard; callers must
	// consult Caps before using them.
	GetObjectPropValue(handle uint32, propCode uint16, value interface{}) error
	SetObjectPropValue(handle uint32, propCode uint16, value interface{}) error
	GetObjectPropsSupported(objFormatCode uint16, props *mtp.Uint16Array) error

	// Data transfer.
	GetObject(handle uint32, w io.Writer, progress mtp.ProgressFunc) error
	GetPartialObject(handle uint32, w io.Writer, offset, size uint32) error
	SendObjectInfo(storageID, parent uint32, info *mtp.ObjectInfo) (outStorageID, outParent, handle uint32, err error)
	SendObject(r io.Reader, size int64, progress mtp.ProgressFunc) error
	DeleteObject(handle uint32) error

	// Escape hatch for operations the engine does not wrap (GetObjectPropList,
	// MoveObject, CopyObject).
	RunTransaction(req *mtp.Container, rep *mtp.Container, dest io.Writer, src io.Reader, writeSize int64, progress mtp.ProgressFunc) error

	// SetTimeout adjusts the USB transfer timeout. The Switch needs a longer
	// timeout than the MTP default when writing to a slow SD card.
	SetTimeout(ms int)
}

// deviceTransport adapts *mtp.Device to Transport.
//
// Almost every method is a straight pass-through; the adapter exists so that
// Transport can carry SetTimeout (a field on mtp.Device, not a method) and so
// that the concrete engine type never leaks into the layers above.
type deviceTransport struct {
	dev *mtp.Device
}

// NewTransport wraps an opened mtp.Device.
func NewTransport(dev *mtp.Device) Transport { return &deviceTransport{dev: dev} }

func (t *deviceTransport) OpenSession() error  { return t.dev.OpenSession() }
func (t *deviceTransport) CloseSession() error { return t.dev.CloseSession() }
func (t *deviceTransport) Close() error        { return t.dev.Close() }
func (t *deviceTransport) Done()               { t.dev.Done() }

func (t *deviceTransport) GetDeviceInfo(info *mtp.DeviceInfo) error {
	return t.dev.GetDeviceInfo(info)
}
func (t *deviceTransport) GetUsbInfo() (*mtp.UsbDeviceInfo, error) { return t.dev.GetUsbInfo() }
func (t *deviceTransport) ID() (string, error)                     { return t.dev.ID() }

func (t *deviceTransport) GetStorageIDs(info *mtp.Uint32Array) error {
	return t.dev.GetStorageIDs(info)
}
func (t *deviceTransport) GetStorageInfo(id uint32, info *mtp.StorageInfo) error {
	return t.dev.GetStorageInfo(id, info)
}

func (t *deviceTransport) GetObjectHandles(storageID, objFormatCode, parent uint32, info *mtp.Uint32Array) error {
	return t.dev.GetObjectHandles(storageID, objFormatCode, parent, info)
}
func (t *deviceTransport) GetObjectInfo(handle uint32, info *mtp.ObjectInfo) error {
	return t.dev.GetObjectInfo(handle, info)
}
func (t *deviceTransport) GetNumObjects(storageID uint32, formatCode uint16, parent uint32) (uint32, error) {
	return t.dev.GetNumObjects(storageID, formatCode, parent)
}

func (t *deviceTransport) GetObjectPropValue(handle uint32, propCode uint16, value interface{}) error {
	return t.dev.GetObjectPropValue(handle, propCode, value)
}
func (t *deviceTransport) SetObjectPropValue(handle uint32, propCode uint16, value interface{}) error {
	return t.dev.SetObjectPropValue(handle, propCode, value)
}
func (t *deviceTransport) GetObjectPropsSupported(objFormatCode uint16, props *mtp.Uint16Array) error {
	return t.dev.GetObjectPropsSupported(objFormatCode, props)
}

func (t *deviceTransport) GetObject(handle uint32, w io.Writer, progress mtp.ProgressFunc) error {
	return t.dev.GetObject(handle, w, progress)
}
func (t *deviceTransport) GetPartialObject(handle uint32, w io.Writer, offset, size uint32) error {
	return t.dev.GetPartialObject(handle, w, offset, size)
}
func (t *deviceTransport) SendObjectInfo(storageID, parent uint32, info *mtp.ObjectInfo) (uint32, uint32, uint32, error) {
	return t.dev.SendObjectInfo(storageID, parent, info)
}
func (t *deviceTransport) SendObject(r io.Reader, size int64, progress mtp.ProgressFunc) error {
	return t.dev.SendObject(r, size, progress)
}
func (t *deviceTransport) DeleteObject(handle uint32) error { return t.dev.DeleteObject(handle) }

func (t *deviceTransport) RunTransaction(req *mtp.Container, rep *mtp.Container, dest io.Writer, src io.Reader, writeSize int64, progress mtp.ProgressFunc) error {
	return t.dev.RunTransaction(req, rep, dest, src, writeSize, progress)
}

func (t *deviceTransport) SetTimeout(ms int) { t.dev.Timeout = ms }
