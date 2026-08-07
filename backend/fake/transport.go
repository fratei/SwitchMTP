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

package fake

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// seedTime pins every seeded timestamp so listing assertions are stable.
var seedTime = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

// associationGenericFolder is AssociationType 0x0001 from the MTP spec. The
// vendored package defines the format code but not the association types.
const associationGenericFolder = 0x0001

// normParent maps the wire representation of "the storage root" onto the
// internal one.
//
// MTP is asymmetric here: GetObjectHandles and SendObjectInfo address the root
// as 0xFFFFFFFF, but ObjectInfo.ParentObject reports it as 0. Devices that get
// this wrong are a classic source of "the root looks empty" bugs, so the fake
// reproduces the real convention exactly.
func normParent(p uint32) uint32 {
	if p == 0xFFFFFFFF {
		return 0
	}
	return p
}

// rc returns the error type the real engine produces for a non-OK response
// code. Matching that concrete type matters: nxmtp classifies errors by
// asserting on mtp.RCError, so a fake returning a plain error would exercise a
// different code path than hardware does.
//
// The op name is deliberately dropped -- RCError carries only the code, and
// the fake should not be more informative than the real device.
func rc(code uint16, op string) error {
	_ = op
	return mtp.RCError(code)
}

func (d *Device) count(op string) {
	d.Calls[op]++
}

// check enforces the preconditions every operation shares.
func (d *Device) check(op string) error {
	if d.closed {
		return fmt.Errorf("mtp: device closed")
	}
	if d.opts.Disconnected {
		return mtp.SyncError(fmt.Sprintf("%s: LIBUSB_ERROR_NO_DEVICE", op))
	}
	if !d.sessionOpen {
		return rc(mtp.RC_SessionNotOpen, op)
	}
	return nil
}

// --- session ------------------------------------------------------------

func (d *Device) OpenSession() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.opts.Disconnected {
		return mtp.SyncError("OpenSession: LIBUSB_ERROR_NO_DEVICE")
	}
	d.sessionOpen = true
	return nil
}

// EnsureSession mirrors the real transport: opening a session that is already
// open is an error, so callers that cannot know whether a lower layer opened
// one ask for this instead.
func (d *Device) EnsureSession() error {
	d.mu.Lock()
	alreadyOpen := d.sessionOpen
	d.mu.Unlock()
	if alreadyOpen {
		return nil
	}
	return d.OpenSession()
}

func (d *Device) CloseSession() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sessionOpen = false
	return nil
}

func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	d.sessionOpen = false
	return nil
}

func (d *Device) Done() {}

func (d *Device) SetTimeout(ms int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.timeoutMs = ms
}

// Timeout reports the timeout the client configured, for assertions about the
// Switch profile applying a longer one than the MTP default.
func (d *Device) Timeout() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.timeoutMs
}

// --- identity -----------------------------------------------------------

func (d *Device) GetDeviceInfo(info *mtp.DeviceInfo) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.opts.Disconnected {
		return mtp.SyncError("GetDeviceInfo: LIBUSB_ERROR_NO_DEVICE")
	}
	d.count("GetDeviceInfo")

	*info = mtp.DeviceInfo{
		StandardVersion:     100,
		MTPVersion:          100,
		MTPExtension:        "microsoft.com: 1.0; android.com: 1.0;",
		Manufacturer:        "Nintendo",
		Model:               "Nintendo Switch",
		DeviceVersion:       "DBI 658",
		SerialNumber:        "XAW10012345678",
		OperationsSupported: d.operations(),
	}
	return nil
}

// operations builds the advertised operation set, honouring the Options that
// hide the optional ones.
func (d *Device) operations() []uint16 {
	ops := []uint16{
		mtp.OC_GetDeviceInfo, mtp.OC_OpenSession, mtp.OC_CloseSession,
		mtp.OC_GetStorageIDs, mtp.OC_GetStorageInfo, mtp.OC_GetNumObjects,
		mtp.OC_GetObjectHandles, mtp.OC_GetObjectInfo, mtp.OC_GetObject,
		mtp.OC_DeleteObject, mtp.OC_SendObjectInfo, mtp.OC_SendObject,
	}
	if !d.opts.NoPropList {
		ops = append(ops, mtp.OC_MTP_GetObjPropList)
	}
	if !d.opts.NoPropValue {
		ops = append(ops, mtp.OC_MTP_GetObjectPropValue)
	}
	if !d.opts.NoSetPropValue {
		ops = append(ops, mtp.OC_MTP_SetObjectPropValue)
	}
	return ops
}

func (d *Device) GetUsbInfo() (*mtp.UsbDeviceInfo, error) {
	return &mtp.UsbDeviceInfo{
		IdVendor:     0x057E,
		IdProduct:    0x201D,
		Manufacturer: "Nintendo",
		Product:      "Nintendo Switch",
		SerialNumber: "XAW10012345678",
	}, nil
}

func (d *Device) ID() (string, error) { return "fake-switch", nil }

// --- storage ------------------------------------------------------------

func (d *Device) GetStorageIDs(info *mtp.Uint32Array) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("GetStorageIDs"); err != nil {
		return err
	}
	d.count("GetStorageIDs")
	info.Values = append([]uint32(nil), d.storages...)
	return nil
}

func (d *Device) GetStorageInfo(id uint32, info *mtp.StorageInfo) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("GetStorageInfo"); err != nil {
		return err
	}
	d.count("GetStorageInfo")
	si, ok := d.storageInf[id]
	if !ok {
		return rc(mtp.RC_InvalidStorageId, "GetStorageInfo")
	}
	*info = *si
	return nil
}

// --- enumeration --------------------------------------------------------

func (d *Device) GetObjectHandles(storageID, objFormatCode, parent uint32, info *mtp.Uint32Array) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("GetObjectHandles"); err != nil {
		return err
	}
	d.count("GetObjectHandles")

	if _, ok := d.storageInf[storageID]; !ok && storageID != 0xFFFFFFFF {
		return rc(mtp.RC_InvalidStorageId, "GetObjectHandles")
	}
	// Install storages accept writes but cannot be listed: DBI answers with an
	// empty set rather than an error, so the UI must not treat them as folders.
	if isInstall(storageID) {
		info.Values = nil
		return nil
	}

	want := normParent(parent)
	for _, n := range d.sorted() {
		if n.storage == storageID && n.parent == want {
			info.Values = append(info.Values, n.handle)
		}
	}
	return nil
}

func (d *Device) GetNumObjects(storageID uint32, formatCode uint16, parent uint32) (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("GetNumObjects"); err != nil {
		return 0, err
	}
	d.count("GetNumObjects")
	var n uint32
	want := normParent(parent)
	for _, x := range d.nodes {
		if x.storage == storageID && x.parent == want {
			n++
		}
	}
	return n, nil
}

func (d *Device) GetObjectInfo(handle uint32, info *mtp.ObjectInfo) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("GetObjectInfo"); err != nil {
		return err
	}
	d.count("GetObjectInfo")

	n, ok := d.nodes[handle]
	if !ok {
		return rc(mtp.RC_InvalidObjectHandle, "GetObjectInfo")
	}
	*info = *d.objectInfo(n)
	return nil
}

func (d *Device) objectInfo(n *node) *mtp.ObjectInfo {
	info := &mtp.ObjectInfo{
		StorageID:        n.storage,
		ParentObject:     n.parent,
		Filename:         n.name,
		ModificationDate: n.modified,
		CaptureDate:      n.modified,
	}
	if n.isDir {
		info.ObjectFormat = mtp.OFC_Association
		info.AssociationType = associationGenericFolder
		return info
	}
	info.ObjectFormat = mtp.OFC_Undefined
	info.CompressedSize = clampToUint32(n.size())
	return info
}

// clampToUint32 reproduces the overflow in ObjectInfo's 32-bit size field.
// A real responder has no choice; the client has to notice the sentinel and
// ask for the true size another way.
func clampToUint32(v int64) uint32 {
	if v >= 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(v)
}

func (n *node) size() int64 {
	if n.declaredSize > 0 {
		return n.declaredSize
	}
	return int64(len(n.data))
}

func (d *Device) sorted() []*node {
	out := make([]*node, 0, len(d.nodes))
	for _, n := range d.nodes {
		out = append(out, n)
	}
	// Handles are allocated in insertion order, so sorting by handle gives a
	// deterministic listing without imposing an ordering real devices do not
	// guarantee.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].handle < out[j-1].handle; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func isInstall(sid uint32) bool { return sid == SidSDInstall || sid == SidNandInstall }

func isReadOnly(sid uint32) bool {
	switch sid {
	case SidNandUser, SidNandSystem, SidInstalledGames, SidAlbum, SidGamecard:
		return true
	}
	return false
}

// --- object properties --------------------------------------------------

func (d *Device) GetObjectPropValue(handle uint32, propCode uint16, value interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("GetObjectPropValue"); err != nil {
		return err
	}
	d.count("GetObjectPropValue")

	if d.opts.NoPropValue || d.opts.SecretlyUnsupported {
		return rc(mtp.RC_OperationNotSupported, "GetObjectPropValue")
	}
	n, ok := d.nodes[handle]
	if !ok {
		return rc(mtp.RC_InvalidObjectHandle, "GetObjectPropValue")
	}
	// The value types must match what the real engine passes, or the client
	// sees a refusal where hardware would answer -- and silently demotes a
	// capability that actually works.
	switch propCode {
	case mtp.OPC_ObjectSize:
		p, ok := value.(*mtp.Uint64Value)
		if !ok {
			return rc(mtp.RC_MTP_Invalid_ObjectProp_Format, "GetObjectPropValue")
		}
		p.Value = uint64(n.size())
		return nil
	case mtp.OPC_ObjectFileName:
		p, ok := value.(*mtp.StringValue)
		if !ok {
			return rc(mtp.RC_MTP_Invalid_ObjectProp_Format, "GetObjectPropValue")
		}
		p.Value = n.name
		return nil
	}
	return rc(mtp.RC_MTP_Invalid_ObjectPropCode, "GetObjectPropValue")
}

func (d *Device) SetObjectPropValue(handle uint32, propCode uint16, value interface{}) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("SetObjectPropValue"); err != nil {
		return err
	}
	d.count("SetObjectPropValue")

	if d.opts.NoSetPropValue || d.opts.SecretlyUnsupported {
		return rc(mtp.RC_OperationNotSupported, "SetObjectPropValue")
	}
	n, ok := d.nodes[handle]
	if !ok {
		return rc(mtp.RC_InvalidObjectHandle, "SetObjectPropValue")
	}
	if isReadOnly(n.storage) {
		return rc(mtp.RC_StoreReadOnly, "SetObjectPropValue")
	}
	if propCode != mtp.OPC_ObjectFileName {
		return rc(mtp.RC_MTP_Invalid_ObjectPropCode, "SetObjectPropValue")
	}
	sv, ok := value.(*mtp.StringValue)
	if !ok {
		return rc(mtp.RC_MTP_Invalid_ObjectProp_Format, "SetObjectPropValue")
	}
	n.name = sv.Value
	return nil
}

func (d *Device) GetObjectPropsSupported(objFormatCode uint16, props *mtp.Uint16Array) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("GetObjectPropsSupported"); err != nil {
		return err
	}
	d.count("GetObjectPropsSupported")
	if d.opts.NoPropValue {
		return rc(mtp.RC_OperationNotSupported, "GetObjectPropsSupported")
	}
	props.Values = []uint16{
		mtp.OPC_StorageID, mtp.OPC_ObjectFormat, mtp.OPC_ObjectSize,
		mtp.OPC_ObjectFileName, mtp.OPC_ParentObject,
	}
	return nil
}

// --- transfer -----------------------------------------------------------

func (d *Device) GetObject(handle uint32, w io.Writer, progress mtp.ProgressFunc) error {
	d.mu.Lock()
	if err := d.check("GetObject"); err != nil {
		d.mu.Unlock()
		return err
	}
	d.count("GetObject")
	n, ok := d.nodes[handle]
	if !ok {
		d.mu.Unlock()
		return rc(mtp.RC_InvalidObjectHandle, "GetObject")
	}
	if n.isDir {
		d.mu.Unlock()
		return rc(mtp.RC_InvalidObjectHandle, "GetObject")
	}
	data := n.data
	d.mu.Unlock()

	// Chunked so progress callbacks fire more than once, which is what the
	// progress arithmetic tests need.
	const chunk = 4096
	var sent int64
	for off := 0; off < len(data); off += chunk {
		end := off + chunk
		if end > len(data) {
			end = len(data)
		}
		nw, err := w.Write(data[off:end])
		sent += int64(nw)
		if err != nil {
			return err
		}
		if progress != nil {
			if err := progress(sent); err != nil {
				return err
			}
		}
	}
	if len(data) == 0 && progress != nil {
		return progress(0)
	}
	return nil
}

func (d *Device) GetPartialObject(handle uint32, w io.Writer, offset, size uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("GetPartialObject"); err != nil {
		return err
	}
	d.count("GetPartialObject")
	n, ok := d.nodes[handle]
	if !ok {
		return rc(mtp.RC_InvalidObjectHandle, "GetPartialObject")
	}
	if int(offset) >= len(n.data) {
		return nil
	}
	end := int(offset) + int(size)
	if end > len(n.data) {
		end = len(n.data)
	}
	_, err := w.Write(n.data[offset:end])
	return err
}

func (d *Device) SendObjectInfo(storageID, parent uint32, info *mtp.ObjectInfo) (uint32, uint32, uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("SendObjectInfo"); err != nil {
		return 0, 0, 0, err
	}
	d.count("SendObjectInfo")

	if _, ok := d.storageInf[storageID]; !ok {
		return 0, 0, 0, rc(mtp.RC_InvalidStorageId, "SendObjectInfo")
	}
	if isReadOnly(storageID) {
		return 0, 0, 0, rc(mtp.RC_StoreReadOnly, "SendObjectInfo")
	}
	// Install storages are flat drop targets; anything but a file at the root
	// is rejected, which is what forces the client to normalise the path.
	parent = normParent(parent)
	if isInstall(storageID) {
		if parent != 0 {
			return 0, 0, 0, rc(mtp.RC_InvalidParentObject, "SendObjectInfo")
		}
		if info.ObjectFormat == mtp.OFC_Association {
			return 0, 0, 0, rc(mtp.RC_AccessDenied, "SendObjectInfo")
		}
	}

	isDir := info.ObjectFormat == mtp.OFC_Association
	n := d.addNode(storageID, parent, info.Filename, isDir, nil)
	n.modified = info.ModificationDate
	if n.modified.IsZero() {
		n.modified = seedTime
	}
	d.pending = n
	return storageID, parent, n.handle, nil
}

func (d *Device) SendObject(r io.Reader, size int64, progress mtp.ProgressFunc) error {
	d.mu.Lock()
	if err := d.check("SendObject"); err != nil {
		d.mu.Unlock()
		return err
	}
	d.count("SendObject")
	n := d.pending
	d.pending = nil
	opts := d.opts
	d.mu.Unlock()

	if n == nil {
		return rc(mtp.RC_GeneralError, "SendObject")
	}

	var buf bytes.Buffer
	const chunk = 4096
	tmp := make([]byte, chunk)
	var received int64
	for {
		nr, err := r.Read(tmp)
		if nr > 0 {
			if opts.FailSendObjectAfter > 0 && received+int64(nr) > opts.FailSendObjectAfter {
				// DBI's applet-mode buffer exhaustion surfaces as a generic
				// device error partway through the transfer, not up front.
				return rc(mtp.RC_GeneralError, "SendObject: extra buffers exceeded")
			}
			buf.Write(tmp[:nr])
			received += int64(nr)
			if opts.SlowWrite > 0 {
				time.Sleep(opts.SlowWrite)
			}
			if progress != nil {
				if perr := progress(received); perr != nil {
					return perr
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if isInstall(n.storage) {
		// An install consumes the object rather than storing it: nothing is
		// browsable afterwards, and the Switch keeps working after the
		// transaction completes.
		delete(d.nodes, n.handle)
		d.Installed = append(d.Installed, n.name)
		return nil
	}
	n.data = buf.Bytes()
	return nil
}

func (d *Device) DeleteObject(handle uint32) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("DeleteObject"); err != nil {
		return err
	}
	d.count("DeleteObject")

	n, ok := d.nodes[handle]
	if !ok {
		return rc(mtp.RC_InvalidObjectHandle, "DeleteObject")
	}
	if isReadOnly(n.storage) {
		return rc(mtp.RC_ObjectWriteProtected, "DeleteObject")
	}
	for _, child := range d.nodes {
		if child.parent == handle {
			return rc(mtp.RC_AccessDenied, "DeleteObject: not empty")
		}
	}
	delete(d.nodes, handle)
	return nil
}

// RunTransaction is the escape hatch for operations the engine does not wrap.
//
// The fake refuses everything: nxmtp only uses it for GetObjectPropList,
// MoveObject and CopyObject, all of which must degrade to a fallback path when
// unavailable. Returning OperationNotSupported here is therefore the most
// useful default, and NoPropList is what a test flips to compare the two
// listing paths.
func (d *Device) RunTransaction(req *mtp.Container, rep *mtp.Container, dest io.Writer, src io.Reader, writeSize int64, progress mtp.ProgressFunc) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check("RunTransaction"); err != nil {
		return err
	}
	d.count(opName(req.Code))
	return rc(mtp.RC_OperationNotSupported, opName(req.Code))
}

func opName(code uint16) string {
	if n, ok := mtp.OC_names[int(code)]; ok {
		return n
	}
	return fmt.Sprintf("op_%04x", code)
}

// --- assertions ---------------------------------------------------------

// Tree renders the device contents as sorted "storage:/path" lines, for
// golden comparisons in tests.
func (d *Device) Tree() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for _, n := range d.sorted() {
		out = append(out, fmt.Sprintf("%d:%s", n.storage, d.pathOf(n)))
	}
	return out
}

func (d *Device) pathOf(n *node) string {
	parts := []string{n.name}
	for p := n.parent; p != 0; {
		parent, ok := d.nodes[p]
		if !ok {
			break
		}
		parts = append([]string{parent.name}, parts...)
		p = parent.parent
	}
	return "/" + strings.Join(parts, "/")
}

// Find returns the node at a storage-relative path, or nil.
func (d *Device) Find(storage uint32, path string) *node {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, n := range d.nodes {
		if n.storage == storage && d.pathOf(n) == path {
			return n
		}
	}
	return nil
}

// Data returns the bytes stored at a path.
func (d *Device) Data(storage uint32, path string) ([]byte, bool) {
	n := d.Find(storage, path)
	if n == nil {
		return nil, false
	}
	return n.data, true
}

// SetDisconnected simulates the cable being pulled mid-session.
func (d *Device) SetDisconnected(v bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.opts.Disconnected = v
}
