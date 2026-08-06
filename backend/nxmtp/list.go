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
	"time"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// sizeOverflow is the value CompressedSize takes when the real size does not
// fit in MTP's 32-bit field. Anything at or above this is a lie.
const sizeOverflow uint32 = 0xFFFFFFFF

// rawEntry is one directory entry as read from the device.
type rawEntry struct {
	Handle   uint32
	Name     string
	IsFolder bool
	Size     int64
	// SizeUnknown is set when the object is larger than 4 GiB and the device
	// would not tell us the real size. Reporting 4294967295 bytes would be
	// actively misleading, so the UI shows a dash instead.
	SizeUnknown bool
	Modified    time.Time
	Format      uint16
	Parent      uint32
}

// FileInfo is a directory entry as returned across the FFI boundary.
//
// The lower-cased field names are fixed by the existing Swift client; only
// sizeUnknown is new.
type FileInfo struct {
	Size        int64     `json:"size"`
	IsFolder    bool      `json:"isFolder"`
	DateAdded   time.Time `json:"dateAdded"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	ParentPath  string    `json:"parentPath"`
	Extension   string    `json:"extension"`
	ParentID    uint32    `json:"parentId"`
	ObjectID    uint32    `json:"objectId"`
	SizeUnknown bool      `json:"sizeUnknown"`
}

// listRaw returns the direct children of a directory.
//
// Two strategies are used. If the device advertises GetObjectPropList (MTP
// operation 0x9805) we fetch every child's properties in a single round trip,
// which is dramatically faster over USB than one GetObjectInfo per entry. If
// it does not -- or if the call turns out to be broken despite being
// advertised -- we fall back to GetObjectHandles + GetObjectInfo.
//
// Whatever happens, an unsupported optional operation degrades the result; it
// never fails the listing.
func (c *Client) listRaw(storageID, parent uint32) ([]rawEntry, error) {
	if c.caps.GetObjectPropList {
		entries, err := c.listViaPropList(storageID, parent)
		if err == nil {
			return entries, nil
		}
		if !IsUnsupported(err) {
			return nil, classify("list", err)
		}
		// Advertised but not actually implemented. Stop trying.
		c.demoteCap(mtp.OC_MTP_GetObjPropList)
	}
	return c.listViaObjectInfo(storageID, parent)
}

// listViaObjectInfo is the universally-supported path: enumerate handles, then
// ask about each one individually.
func (c *Client) listViaObjectInfo(storageID, parent uint32) ([]rawEntry, error) {
	var handles mtp.Uint32Array
	if err := c.t.GetObjectHandles(storageID, 0, parent, &handles); err != nil {
		return nil, classify("list", err)
	}

	entries := make([]rawEntry, 0, len(handles.Values))
	for _, h := range handles.Values {
		if err := c.checkCancelled(); err != nil {
			return nil, err
		}
		var info mtp.ObjectInfo
		if err := c.t.GetObjectInfo(h, &info); err != nil {
			if IsDisconnected(err) {
				return nil, classify("list", err)
			}
			// A single unreadable entry must not sink the whole directory;
			// DBI's virtual storages occasionally expose handles that cannot
			// be described.
			continue
		}
		entries = append(entries, c.entryFromObjectInfo(h, parent, &info))
	}
	return entries, nil
}

// entryFromObjectInfo converts an ObjectInfo, resolving the true size when the
// 32-bit field has overflowed.
func (c *Client) entryFromObjectInfo(handle, parent uint32, info *mtp.ObjectInfo) rawEntry {
	e := rawEntry{
		Handle:   handle,
		Name:     info.Filename,
		IsFolder: isAssociation(info.ObjectFormat),
		Size:     int64(info.CompressedSize),
		Modified: pickDate(info),
		Format:   info.ObjectFormat,
		Parent:   parent,
	}

	if !e.IsFolder && info.CompressedSize >= sizeOverflow {
		if size, ok := c.trueSize(handle); ok {
			e.Size = size
		} else {
			e.Size = 0
			e.SizeUnknown = true
		}
	}
	if e.IsFolder {
		e.Size = 0
	}
	return e
}

// trueSize asks for the real 64-bit object size. Only attempted when the
// device advertised GetObjectPropValue; a runtime refusal demotes the
// capability so we stop asking for the rest of the session.
//
// DBI's support for this is not documented, which is exactly why the caller
// must tolerate a false return rather than assuming a size.
func (c *Client) trueSize(handle uint32) (int64, bool) {
	if !c.caps.GetObjectPropValue {
		return 0, false
	}
	var v mtp.Uint64Value
	if err := c.t.GetObjectPropValue(handle, mtp.OPC_ObjectSize, &v); err != nil {
		if IsUnsupported(err) {
			c.demoteCap(mtp.OC_MTP_GetObjectPropValue)
		}
		return 0, false
	}
	if v.Value >= uint64(sizeOverflow) || v.Value == 0 {
		// Some responders echo the overflowed value back; that is not an
		// answer.
		if v.Value == 0 {
			return 0, false
		}
	}
	return int64(v.Value), true
}

// pickDate chooses the most meaningful timestamp an ObjectInfo carries.
func pickDate(info *mtp.ObjectInfo) time.Time {
	if !info.ModificationDate.IsZero() && info.ModificationDate.Year() > 1970 {
		return info.ModificationDate
	}
	if !info.CaptureDate.IsZero() && info.CaptureDate.Year() > 1970 {
		return info.CaptureDate
	}
	return time.Time{}
}

// toFileInfo converts a raw entry into the FFI-facing shape.
func toFileInfo(e rawEntry, parentPath string) FileInfo {
	full := JoinPath(parentPath, e.Name)
	ext := ""
	if !e.IsFolder {
		ext = strings.TrimPrefix(strings.ToLower(path.Ext(e.Name)), ".")
	}
	return FileInfo{
		Size:        e.Size,
		IsFolder:    e.IsFolder,
		DateAdded:   e.Modified,
		Name:        e.Name,
		Path:        full,
		ParentPath:  NormalizePath(parentPath),
		Extension:   ext,
		ParentID:    e.Parent,
		ObjectID:    e.Handle,
		SizeUnknown: e.SizeUnknown,
	}
}

// WalkOptions controls a directory traversal.
type WalkOptions struct {
	StorageID           uint32
	FullPath            string
	Recursive           bool
	SkipHiddenFiles     bool
	SkipDisallowedFiles bool
}

// disallowedNames are filesystem artefacts that are never interesting to show.
var disallowedNames = map[string]bool{
	".ds_store":                 true,
	"thumbs.db":                 true,
	"desktop.ini":               true,
	".spotlight-v100":           true,
	".trashes":                  true,
	".fseventsd":                true,
	"system volume information": true,
}

func shouldSkip(name string, opts WalkOptions) bool {
	lower := strings.ToLower(name)
	if opts.SkipDisallowedFiles && disallowedNames[lower] {
		return true
	}
	if opts.SkipHiddenFiles && strings.HasPrefix(name, ".") {
		return true
	}
	return false
}

// Walk lists a directory, optionally recursing.
//
// Storages that are install-only drop targets are rejected outright: DBI
// exposes them as writable but they contain nothing, and browsing them
// produces confusing empty views or errors.
func (c *Client) Walk(opts WalkOptions) ([]FileInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	st, err := c.storageByID(opts.StorageID)
	if err != nil {
		return nil, err
	}
	if !st.Capabilities.Browse {
		if st.Capabilities.InstallTarget {
			return nil, &Error{
				Kind: KindWriteOnly,
				Op:   "walk",
				Msg:  "this storage cannot be browsed",
				Hint: "\"" + st.DisplayName + "\" is an install target. Copy an NSP, NSZ, XCI or XCZ to it to start an installation.",
			}
		}
		return nil, errf(KindUnsupported, "walk", "storage %q cannot be browsed", st.DisplayName)
	}

	root := NormalizePath(opts.FullPath)
	node, err := c.resolvePath(opts.StorageID, root)
	if err != nil {
		return nil, err
	}
	if !node.isFolder {
		return nil, errf(KindInvalidInput, "walk", "%q is not a directory", root)
	}

	var out []FileInfo
	err = c.walkInto(opts, root, node.handle, &out)
	return out, err
}

func (c *Client) walkInto(opts WalkOptions, dirPath string, handle uint32, out *[]FileInfo) error {
	if err := c.checkCancelled(); err != nil {
		return err
	}

	entries, err := c.listRaw(opts.StorageID, handle)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if shouldSkip(e.Name, opts) {
			continue
		}
		fi := toFileInfo(e, dirPath)
		c.cache.put(opts.StorageID, fi.Path, cachedNode{handle: e.Handle, isFolder: e.IsFolder})
		*out = append(*out, fi)

		if e.IsFolder && opts.Recursive {
			if err := c.walkInto(opts, fi.Path, e.Handle, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// FileExistsResult reports whether a path exists on the device.
type FileExistsResult struct {
	FullPath string `json:"fullpath"`
	Exists   bool   `json:"exists"`
}

// FileExists checks a batch of paths.
func (c *Client) FileExists(storageID uint32, paths []string) ([]FileExistsResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.storageByID(storageID); err != nil {
		return nil, err
	}

	out := make([]FileExistsResult, 0, len(paths))
	for _, p := range paths {
		norm := NormalizePath(p)
		_, err := c.resolvePath(storageID, norm)
		switch {
		case err == nil:
			out = append(out, FileExistsResult{FullPath: norm, Exists: true})
		case KindOf(err) == KindNotFound:
			out = append(out, FileExistsResult{FullPath: norm, Exists: false})
		default:
			return nil, err
		}
	}
	return out, nil
}
