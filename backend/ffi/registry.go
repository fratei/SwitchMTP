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
	"sync"

	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

// registry holds the open device sessions.
//
// MTP allows one session per device, so a client is cached and reused across
// FFI calls rather than reconnecting each time: reconnecting per operation
// would be both slow and, on DBI, unreliable.
type registry struct {
	mu      sync.Mutex
	clients map[string]*nxmtp.Client
}

var reg = &registry{clients: make(map[string]*nxmtp.Client)}

// get returns the open client for a device id.
func (r *registry) get(deviceID string) (*nxmtp.Client, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[deviceID]
	return c, ok
}

// open returns the existing client for a device, or connects a new one.
//
// The registry lock is held across the connect so two concurrent Initialize
// calls for the same device cannot both open a session; the loser would
// otherwise leave an orphaned USB handle behind.
func (r *registry) open(deviceID string) (*nxmtp.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if c, ok := r.clients[deviceID]; ok {
		// A cached client is only worth returning if its session still works.
		// Initialize is an explicit "connect me" request, and answering it from
		// a client whose USB handle has closed reports success from cached
		// device info while every real operation afterwards fails. One probe
		// here is cheap next to a connect the user has to ask for twice.
		if err := c.Validate(); err == nil {
			return c, nil
		}
		delete(r.clients, deviceID)
		_ = c.Close()
	}
	c, err := nxmtp.Open(deviceID)
	if err != nil {
		return nil, err
	}
	r.clients[deviceID] = c
	return c, nil
}

// require returns the open client for a device, connecting on demand.
//
// Operations arriving for a device the app believes is connected must not fail
// merely because the session was dropped (an unplug, a sleep/wake cycle, or a
// previous fatal error). Reconnecting transparently is what makes the app
// survive the replug that DBI on macOS so often needs.
func (r *registry) require(deviceID string) (*nxmtp.Client, error) {
	if c, ok := r.get(deviceID); ok {
		return c, nil
	}
	return r.open(deviceID)
}

// close disposes of one device session.
func (r *registry) close(deviceID string) {
	r.mu.Lock()
	c, ok := r.clients[deviceID]
	delete(r.clients, deviceID)
	r.mu.Unlock()

	if ok && c != nil {
		_ = c.Close()
	}
}

// drop removes a client without closing it, used when the session is already
// known to be dead.
func (r *registry) drop(deviceID string) {
	r.mu.Lock()
	delete(r.clients, deviceID)
	r.mu.Unlock()
}

// closeAll tears down every session.
func (r *registry) closeAll() {
	r.mu.Lock()
	clients := make([]*nxmtp.Client, 0, len(r.clients))
	for id, c := range r.clients {
		clients = append(clients, c)
		delete(r.clients, id)
	}
	r.mu.Unlock()

	for _, c := range clients {
		_ = c.Close()
	}
}

// cancel aborts the transfer in flight on a device, if any.
func (r *registry) cancel(deviceID string) bool {
	c, ok := r.get(deviceID)
	if !ok {
		return false
	}
	c.Cancel()
	return true
}
