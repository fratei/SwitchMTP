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
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ganeshrvel/go-mtpfs/mtp"
	"github.com/ganeshrvel/usb"
)

// Client is a connected MTP session for one device.
//
// MTP permits exactly one session per device, so every exported method holds
// c.mu for its whole duration. That serialisation is a protocol requirement,
// not a convenience: interleaving two transactions on one device corrupts the
// transaction-id sequence and wedges the responder until it is replugged.
type Client struct {
	mu sync.Mutex

	t     Transport
	dev   *mtp.Device
	usbCx *usb.Context

	ref     DeviceRef
	info    mtp.DeviceInfo
	usbInfo mtp.UsbDeviceInfo
	caps    *Capabilities

	storages     []Storage
	storageIndex map[uint32]*Storage

	cache *pathCache

	// cancel guards the currently running transfer. CancelTransfer trips it
	// from another goroutine, which is safe because only the pointer swap is
	// contended -- the context itself is read-only once installed.
	cancelMu sync.Mutex
	cancelFn context.CancelFunc
	cancelCx context.Context

	closed bool
}

// DeviceDetails is the payload returned by Initialize and FetchDeviceInfo.
//
// The mtpDeviceInfo/usbDeviceInfo keys and their nested field names are fixed
// by the existing Swift client. capabilities, deviceProfile and the rest are
// additive.
type DeviceDetails struct {
	MTPDeviceInfo *mtp.DeviceInfo    `json:"mtpDeviceInfo"`
	USBDeviceInfo *mtp.UsbDeviceInfo `json:"usbDeviceInfo"`

	Capabilities  *Capabilities `json:"capabilities"`
	DeviceProfile DeviceProfile `json:"deviceProfile"`
	DisplayName   string        `json:"displayName"`
	DeviceID      string        `json:"deviceId"`
	Advice        string        `json:"advice,omitempty"`
}

// Open connects to the device identified by a canonical device id and
// negotiates a session.
func Open(deviceID string) (*Client, error) {
	dev, usbCx, ref, err := openByID(deviceID)
	if err != nil {
		return nil, err
	}

	c := &Client{
		dev:          dev,
		usbCx:        usbCx,
		ref:          ref,
		cache:        newPathCache(),
		storageIndex: make(map[uint32]*Storage),
	}

	// A device in a homebrew USB mode never gets this far -- it presents no MTP
	// interface -- but a Switch in some other non-MTP mode might, so refuse
	// before we start a session rather than timing out later.
	if !ref.Usable {
		c.hardClose()
		return nil, &Error{Kind: KindWrongMode, Op: "open", Msg: "device is not in MTP mode", Hint: ref.Advice}
	}

	// Configure() claims the interface and clears any stale halt condition
	// left behind by a previous client that died mid-transaction.
	if err := dev.Configure(); err != nil {
		c.hardClose()
		return nil, &Error{Kind: KindDeviceBusy, Op: "open",
			Msg: "could not configure the device", Hint: occupiedHint(), Err: err}
	}

	// Set a generous timeout before the first transaction: DBI can take a
	// while to answer on a busy console.
	dev.Timeout = timeoutFor(ref.Profile)
	c.t = NewTransport(dev)

	// Configure() has already opened the session, including recovering from a
	// stale one left behind by a client that died mid-transaction. Opening a
	// second session here is not just redundant, it is an error the MTP layer
	// rejects outright -- only ever reached once Configure() starts succeeding,
	// which is why it hid behind connection failures for so long.
	if err := c.t.EnsureSession(); err != nil {
		c.hardClose()
		return nil, &Error{Kind: KindDeviceBusy, Op: "openSession",
			Msg: "the device refused to open an MTP session",
			Hint: "Another application may already have a session open. Quit other MTP or photo apps, " +
				"then unplug and reconnect the Switch.", Err: err}
	}

	if err := c.t.GetDeviceInfo(&c.info); err != nil {
		c.hardClose()
		return nil, classify("getDeviceInfo", err)
	}

	// Now that we can see the MTP identity, distinguish DBI from stock HOS and
	// re-tune the timeout accordingly.
	c.ref.Profile = refineProfile(c.ref.Profile, &c.info)
	c.ref.DisplayName = displayNameFor(c.ref.Profile, c.ref.Manufacturer, c.ref.Model)
	c.t.SetTimeout(timeoutFor(c.ref.Profile))

	c.caps = capsFromDeviceInfo(&c.info)

	if usbInfo, err := c.t.GetUsbInfo(); err == nil && usbInfo != nil {
		c.usbInfo = *usbInfo
	}

	if err := c.refreshStorages(); err != nil {
		c.hardClose()
		return nil, err
	}

	return c, nil
}

// Details returns the device identity and capabilities.
func (c *Client) Details() *DeviceDetails {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.details()
}

func (c *Client) details() *DeviceDetails {
	info := c.info
	usbInfo := c.usbInfo
	return &DeviceDetails{
		MTPDeviceInfo: &info,
		USBDeviceInfo: &usbInfo,
		Capabilities:  c.caps,
		DeviceProfile: c.ref.Profile,
		DisplayName:   c.ref.DisplayName,
		DeviceID:      c.ref.ID(),
		Advice:        c.ref.Advice,
	}
}

// Ref returns the device reference this client was opened with.
func (c *Client) Ref() DeviceRef { return c.ref }

// Caps returns the negotiated capability set.
func (c *Client) Caps() *Capabilities { return c.caps }

// Storages returns the classified storage list, refreshing it from the device.
func (c *Client) Storages() ([]Storage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.refreshStorages(); err != nil {
		return nil, err
	}
	out := make([]Storage, len(c.storages))
	copy(out, c.storages)
	return out, nil
}

// refreshStorages re-reads the storage list. Callers must hold c.mu.
//
// DBI's storage set is not static: inserting a game card adds one, and
// finishing an installation changes the free space on another, so this is
// re-run rather than cached for the session lifetime.
func (c *Client) refreshStorages() error {
	var ids mtp.Uint32Array
	if err := c.t.GetStorageIDs(&ids); err != nil {
		return classify("getStorageIDs", err)
	}

	storages := make([]Storage, 0, len(ids.Values))
	for _, sid := range ids.Values {
		var info mtp.StorageInfo
		if err := c.t.GetStorageInfo(sid, &info); err != nil {
			if IsDisconnected(err) {
				return classify("getStorageInfo", err)
			}
			// A storage that will not describe itself is not usable, but it
			// must not prevent the others from being listed.
			continue
		}
		storages = append(storages, classifyStorage(sid, &info, c.ref.Profile, c.caps))
	}

	// Present in a stable, sensible order: SD card first, install targets
	// grouped, system partitions last.
	sort.SliceStable(storages, func(i, j int) bool {
		if storages[i].Order != storages[j].Order {
			return storages[i].Order < storages[j].Order
		}
		return storages[i].Sid < storages[j].Sid
	})

	c.storages = storages
	c.storageIndex = make(map[uint32]*Storage, len(storages))
	for i := range c.storages {
		c.storageIndex[c.storages[i].Sid] = &c.storages[i]
	}
	return nil
}

// storageByID looks up a storage. Callers must hold c.mu.
func (c *Client) storageByID(sid uint32) (*Storage, error) {
	if st, ok := c.storageIndex[sid]; ok {
		return st, nil
	}
	// The caller may be using a storage id from before a refresh; try once
	// more against the device before giving up.
	if err := c.refreshStorages(); err != nil {
		return nil, err
	}
	if st, ok := c.storageIndex[sid]; ok {
		return st, nil
	}
	return nil, errf(KindNotFound, "storage", "unknown storage id %d", sid)
}

// demoteCap records that an advertised operation does not actually work.
func (c *Client) demoteCap(op uint16) {
	if c.caps.demote(op) {
		logf("device %s does not implement operation 0x%04X despite advertising it; disabling", c.ref.ID(), op)
	}
}

// --- cancellation -------------------------------------------------------

// beginCancellable installs a fresh cancellation context for a transfer.
func (c *Client) beginCancellable() context.Context {
	c.cancelMu.Lock()
	defer c.cancelMu.Unlock()
	if c.cancelFn != nil {
		c.cancelFn()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelCx, c.cancelFn = ctx, cancel
	return ctx
}

// endCancellable tears the context down once a transfer finishes.
func (c *Client) endCancellable() {
	c.cancelMu.Lock()
	defer c.cancelMu.Unlock()
	if c.cancelFn != nil {
		c.cancelFn()
		c.cancelFn = nil
		c.cancelCx = nil
	}
}

// Cancel aborts the transfer currently in flight, if any.
//
// This deliberately does not take c.mu: the whole point is to interrupt an
// operation that is holding it. The transfer loops observe the context between
// chunks and unwind cleanly.
func (c *Client) Cancel() {
	c.cancelMu.Lock()
	fn := c.cancelFn
	c.cancelMu.Unlock()
	if fn != nil {
		fn()
	}
}

// checkCancelled reports whether the in-flight operation has been cancelled.
func (c *Client) checkCancelled() error {
	c.cancelMu.Lock()
	ctx := c.cancelCx
	c.cancelMu.Unlock()
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return newErr(KindCancelled, "transfer", "cancelled")
	default:
		return nil
	}
}

// --- teardown -----------------------------------------------------------

// Close ends the MTP session and releases the USB device.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

func (c *Client) closeLocked() error {
	if c.closed {
		return nil
	}
	c.closed = true
	c.endCancellable()

	// Closing the session politely lets the responder release its own state;
	// DBI in particular is happier on the next connection if we do. A failure
	// here is not worth reporting -- we are tearing down regardless.
	if c.t != nil {
		_ = c.t.CloseSession()
	}
	c.hardClose()
	return nil
}

// hardClose releases USB resources without touching MTP state.
func (c *Client) hardClose() {
	if c.dev != nil {
		_ = c.dev.Close()
		c.dev.Done()
		c.dev = nil
	}
	if c.usbCx != nil {
		c.usbCx.Exit()
		c.usbCx = nil
	}
	c.t = nil
	if c.cache != nil {
		c.cache.reset()
	}
}

// newClientForTesting builds a Client around an arbitrary Transport. It exists
// so the fake device can exercise every layer above the transport without USB.
func newClientForTesting(t Transport, ref DeviceRef, info mtp.DeviceInfo) (*Client, error) {
	c := &Client{
		t:            t,
		ref:          ref,
		info:         info,
		cache:        newPathCache(),
		storageIndex: make(map[uint32]*Storage),
	}
	// Discovery normally assigns the profile from the USB ids before a Client
	// exists. Deriving it here too keeps this constructor faithful to the real
	// path: without it a Switch would be treated as a generic MTP device and
	// every DBI-specific rule would silently not apply.
	if c.ref.Profile == "" {
		c.ref.Profile, _, _ = profileFor(c.ref.VendorID, c.ref.ProductID,
			c.ref.Manufacturer, c.ref.Model)
	}
	c.ref.Profile = refineProfile(c.ref.Profile, &c.info)
	c.caps = capsFromDeviceInfo(&c.info)
	if err := c.refreshStorages(); err != nil {
		return nil, err
	}
	return c, nil
}

// NewClientWithTransport exposes the test constructor to sibling packages.
func NewClientWithTransport(t Transport, ref DeviceRef, info mtp.DeviceInfo) (*Client, error) {
	return newClientForTesting(t, ref, info)
}

// now is indirected so tests can pin timestamps.
var now = time.Now
