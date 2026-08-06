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
	"path"
	"strings"
	"sync"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// ParentRoot is the MTP object handle meaning "the root of the storage".
const ParentRoot uint32 = 0xFFFFFFFF

// pathCache maps a normalised path to the MTP object handle that path resolves
// to, per storage.
//
// Path resolution in MTP is inherently expensive: there is no "look up by
// path" operation, so every segment costs a GetObjectHandles scan of its
// parent plus a GetObjectInfo per entry to read the name. The previous
// generation of this stack re-walked the entire chain on every call, which on
// a Switch SD card with thousands of files made ordinary browsing painful.
//
// The cache is invalidated conservatively: any mutation drops the affected
// subtree, so a stale handle can never outlive a delete or a rename.
type pathCache struct {
	mu      sync.RWMutex
	entries map[uint32]map[string]cachedNode
}

type cachedNode struct {
	handle   uint32
	isFolder bool
}

func newPathCache() *pathCache {
	return &pathCache{entries: make(map[uint32]map[string]cachedNode)}
}

func (c *pathCache) get(storageID uint32, p string) (cachedNode, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.entries[storageID]
	if !ok {
		return cachedNode{}, false
	}
	n, ok := m[p]
	return n, ok
}

func (c *pathCache) put(storageID uint32, p string, n cachedNode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.entries[storageID]
	if !ok {
		m = make(map[string]cachedNode)
		c.entries[storageID] = m
	}
	m[p] = n
}

// invalidate drops p and everything beneath it.
func (c *pathCache) invalidate(storageID uint32, p string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.entries[storageID]
	if !ok {
		return
	}
	p = NormalizePath(p)
	prefix := p
	if prefix != "/" {
		prefix += "/"
	}
	for k := range m {
		if k == p || strings.HasPrefix(k, prefix) {
			delete(m, k)
		}
	}
}

func (c *pathCache) invalidateStorage(storageID uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, storageID)
}

func (c *pathCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[uint32]map[string]cachedNode)
}

// NormalizePath converts any user-supplied path into the canonical form used
// as a cache key and returned to the client: a leading slash, no trailing
// slash, backslashes folded to forward slashes, and no "." or ".." segments.
func NormalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	if p == "." {
		return "/"
	}
	return p
}

// JoinPath joins a parent path and a child name into a canonical path.
func JoinPath(parent, name string) string {
	parent = NormalizePath(parent)
	if parent == "/" {
		return NormalizePath("/" + name)
	}
	return NormalizePath(parent + "/" + name)
}

// splitPath returns the non-empty segments of a canonical path.
func splitPath(p string) []string {
	p = NormalizePath(p)
	if p == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

// resolvePath walks a path down from the storage root, returning the object
// handle it names.
//
// Lookups are case-insensitive on the final comparison because FAT32/exFAT --
// what a Switch SD card uses -- is case-insensitive, and users type paths by
// hand. An exact match always wins over a case-insensitive one.
func (c *Client) resolvePath(storageID uint32, p string) (cachedNode, error) {
	p = NormalizePath(p)
	if p == "/" {
		return cachedNode{handle: ParentRoot, isFolder: true}, nil
	}
	if n, ok := c.cache.get(storageID, p); ok {
		return n, nil
	}

	segments := splitPath(p)
	parent := ParentRoot
	cur := ""

	for i, seg := range segments {
		cur = JoinPath(cur, seg)

		if n, ok := c.cache.get(storageID, cur); ok {
			if !n.isFolder && i < len(segments)-1 {
				return cachedNode{}, errf(KindNotFound, "resolvePath", "%q is not a directory", cur)
			}
			parent = n.handle
			continue
		}

		node, err := c.findChild(storageID, parent, seg)
		if err != nil {
			return cachedNode{}, err
		}
		if !node.isFolder && i < len(segments)-1 {
			return cachedNode{}, errf(KindNotFound, "resolvePath", "%q is not a directory", cur)
		}
		c.cache.put(storageID, cur, node)
		parent = node.handle
	}

	return c.cache.mustGet(storageID, p)
}

func (c *pathCache) mustGet(storageID uint32, p string) (cachedNode, error) {
	n, ok := c.get(storageID, p)
	if !ok {
		return cachedNode{}, errf(KindNotFound, "resolvePath", "%q not found", p)
	}
	return n, nil
}

// findChild scans a directory for an entry with the given name.
func (c *Client) findChild(storageID, parent uint32, name string) (cachedNode, error) {
	entries, err := c.listRaw(storageID, parent)
	if err != nil {
		return cachedNode{}, err
	}
	lower := strings.ToLower(name)
	var fallback *rawEntry
	for i := range entries {
		e := &entries[i]
		if e.Name == name {
			return cachedNode{handle: e.Handle, isFolder: e.IsFolder}, nil
		}
		if fallback == nil && strings.ToLower(e.Name) == lower {
			fallback = e
		}
	}
	if fallback != nil {
		return cachedNode{handle: fallback.Handle, isFolder: fallback.IsFolder}, nil
	}
	return cachedNode{}, errf(KindNotFound, "resolvePath", "%q not found", name)
}

// resolveParent resolves the directory containing p, returning its handle and
// the base name of p. Used by create/rename/delete paths.
func (c *Client) resolveParent(storageID uint32, p string) (parent uint32, name string, err error) {
	p = NormalizePath(p)
	if p == "/" {
		return 0, "", errf(KindInvalidInput, "resolveParent", "cannot operate on the storage root")
	}
	dir, base := path.Split(strings.TrimSuffix(p, "/"))
	node, err := c.resolvePath(storageID, NormalizePath(dir))
	if err != nil {
		return 0, "", err
	}
	if !node.isFolder {
		return 0, "", errf(KindInvalidInput, "resolveParent", "%q is not a directory", dir)
	}
	return node.handle, base, nil
}

// isAssociation reports whether an object format code denotes a directory.
func isAssociation(format uint16) bool {
	return format == mtp.OFC_Association
}
