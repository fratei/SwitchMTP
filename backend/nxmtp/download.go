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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// DownloadRequest describes a download from the device to the local disk.
type DownloadRequest struct {
	StorageID   uint32
	Sources     []string
	Destination string
	// PreprocessFiles asks for a full traversal before transferring, so the
	// UI can show an accurate total. It costs an extra pass over the tree.
	PreprocessFiles bool
}

// TransferSummary is the result of a completed transfer.
type TransferSummary struct {
	TotalFiles       int64    `json:"totalFiles"`
	TotalDirectories int64    `json:"totalDirectories"`
	TotalBytes       int64    `json:"totalBytes"`
	Elapsed          float64  `json:"elapsed"`
	Skipped          []string `json:"skipped,omitempty"`
	Note             string   `json:"note,omitempty"`
}

// PreprocessResult is delivered to the preprocess callback before the bytes
// start moving.
type PreprocessResult struct {
	TotalFiles       int64 `json:"totalFiles"`
	TotalDirectories int64 `json:"totalDirectories"`
	TotalSize        int64 `json:"totalSize"`
	SizeUnknown      bool  `json:"sizeUnknown"`
}

// downloadItem is one file scheduled for download.
type downloadItem struct {
	devicePath  string
	localPath   string
	handle      uint32
	size        int64
	sizeUnknown bool
	modified    time.Time
}

// Download copies files and directories from the device to the local disk.
func (c *Client) Download(req DownloadRequest, onPreprocess func(PreprocessResult), onProgress ProgressFunc) (*TransferSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx := c.beginCancellable()
	defer c.endCancellable()
	_ = ctx

	st, err := c.storageByID(req.StorageID)
	if err != nil {
		return nil, err
	}
	if !st.Capabilities.Read {
		return nil, errf(KindUnsupported, "download", "storage %q cannot be read", st.DisplayName)
	}

	if req.Destination == "" {
		return nil, newErr(KindInvalidInput, "download", "no destination directory given")
	}
	if err := os.MkdirAll(req.Destination, 0o755); err != nil {
		return nil, wrapErr(KindLocalIO, "download", err)
	}

	// Plan the transfer. A virtual storage cannot be sized in advance, so we
	// tell the UI to show an indefinite progress indicator rather than a
	// percentage that would jump around.
	items, dirs, err := c.planDownload(req, st)
	if err != nil {
		return nil, err
	}

	var totalBytes int64
	sizeUnknown := !st.SizeReliable
	for _, it := range items {
		if it.sizeUnknown {
			sizeUnknown = true
		}
		totalBytes += it.size
	}

	if onPreprocess != nil {
		onPreprocess(PreprocessResult{
			TotalFiles:       int64(len(items)),
			TotalDirectories: int64(dirs),
			TotalSize:        totalBytes,
			SizeUnknown:      sizeUnknown,
		})
	}

	tracker := newProgressTracker(onProgress)
	defer tracker.stop()
	tracker.setTotals(int64(len(items)), int64(dirs), totalBytes, sizeUnknown)
	if st.Virtual {
		tracker.setStatus(StatusTransferring, "This storage generates files on demand; the size may not be known in advance.")
	}

	summary := &TransferSummary{
		TotalFiles:       int64(len(items)),
		TotalDirectories: int64(dirs),
	}

	for _, it := range items {
		if err := c.checkCancelled(); err != nil {
			tracker.setStatus(StatusCancelled, "")
			return nil, err
		}
		n, err := c.downloadOne(it, tracker)
		if err != nil {
			tracker.setStatus(StatusFailed, "")
			return nil, err
		}
		summary.TotalBytes += n
	}

	tracker.setStatus(StatusCompleted, "")
	summary.Elapsed = now().Sub(tracker.start).Seconds()
	return summary, nil
}

// planDownload expands the requested sources into a flat list of files,
// creating the local directory structure as it goes.
func (c *Client) planDownload(req DownloadRequest, st *Storage) ([]downloadItem, int, error) {
	var items []downloadItem
	dirs := 0

	for _, src := range req.Sources {
		src = NormalizePath(src)
		node, err := c.resolvePath(req.StorageID, src)
		if err != nil {
			return nil, 0, err
		}

		base := filepath.Base(strings.TrimSuffix(src, "/"))
		if src == "/" {
			base = sanitizeLocalName(st.DisplayName)
		}

		if !node.isFolder {
			info, err := c.statHandle(node.handle)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, downloadItem{
				devicePath:  src,
				localPath:   filepath.Join(req.Destination, sanitizeLocalName(base)),
				handle:      node.handle,
				size:        info.Size,
				sizeUnknown: info.SizeUnknown,
				modified:    info.Modified,
			})
			continue
		}

		localRoot := filepath.Join(req.Destination, sanitizeLocalName(base))
		if err := os.MkdirAll(localRoot, 0o755); err != nil {
			return nil, 0, wrapErr(KindLocalIO, "download", err)
		}
		dirs++
		n, err := c.planDirectory(req.StorageID, src, node.handle, localRoot, &items)
		if err != nil {
			return nil, 0, err
		}
		dirs += n
	}
	return items, dirs, nil
}

// planDirectory recursively expands a device directory.
func (c *Client) planDirectory(storageID uint32, devicePath string, handle uint32, localDir string, items *[]downloadItem) (int, error) {
	if err := c.checkCancelled(); err != nil {
		return 0, err
	}

	entries, err := c.listRaw(storageID, handle)
	if err != nil {
		return 0, err
	}

	dirs := 0
	for _, e := range entries {
		childDevice := JoinPath(devicePath, e.Name)
		childLocal := filepath.Join(localDir, sanitizeLocalName(e.Name))
		c.cache.put(storageID, childDevice, cachedNode{handle: e.Handle, isFolder: e.IsFolder})

		if e.IsFolder {
			if err := os.MkdirAll(childLocal, 0o755); err != nil {
				return dirs, wrapErr(KindLocalIO, "download", err)
			}
			dirs++
			n, err := c.planDirectory(storageID, childDevice, e.Handle, childLocal, items)
			if err != nil {
				return dirs, err
			}
			dirs += n
			continue
		}

		*items = append(*items, downloadItem{
			devicePath:  childDevice,
			localPath:   childLocal,
			handle:      e.Handle,
			size:        e.Size,
			sizeUnknown: e.SizeUnknown,
			modified:    e.Modified,
		})
	}
	return dirs, nil
}

// downloadOne streams a single object to disk.
//
// The file is written to a temporary name and renamed on success, so an
// interrupted or cancelled transfer never leaves a truncated file that looks
// complete. This matters more than usual here: a half-written NSP is
// indistinguishable from a whole one by name alone.
func (c *Client) downloadOne(it downloadItem, tracker *progressTracker) (int64, error) {
	tracker.beginFile(filepath.Base(it.localPath), it.devicePath, it.size)

	if err := os.MkdirAll(filepath.Dir(it.localPath), 0o755); err != nil {
		return 0, wrapErr(KindLocalIO, "download", err)
	}

	tmp := it.localPath + ".switchmtp-part"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, wrapErr(KindLocalIO, "download", err)
	}

	cleanup := func() {
		f.Close()
		os.Remove(tmp)
	}

	var written int64
	progress := func(sent int64) error {
		written = sent
		if err := c.checkCancelled(); err != nil {
			return err
		}
		tracker.advance(sent)
		return nil
	}

	if err := c.t.GetObject(it.handle, f, progress); err != nil {
		cleanup()
		if KindOf(err) == KindCancelled {
			return 0, err
		}
		return 0, classify("download "+it.devicePath, err)
	}

	if err := f.Sync(); err != nil {
		cleanup()
		return 0, wrapErr(KindLocalIO, "download", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return 0, wrapErr(KindLocalIO, "download", err)
	}

	if err := os.Rename(tmp, it.localPath); err != nil {
		os.Remove(tmp)
		return 0, wrapErr(KindLocalIO, "download", err)
	}

	if !it.modified.IsZero() {
		_ = os.Chtimes(it.localPath, it.modified, it.modified)
	}

	tracker.endFile()
	return written, nil
}

// statInfo is a lightweight stat of a device object.
type statInfo struct {
	Name        string
	Size        int64
	SizeUnknown bool
	IsFolder    bool
	Modified    time.Time
}

func (c *Client) statHandle(handle uint32) (statInfo, error) {
	var info mtp.ObjectInfo
	if err := c.t.GetObjectInfo(handle, &info); err != nil {
		return statInfo{}, classify("stat", err)
	}
	e := c.entryFromObjectInfo(handle, info.ParentObject, &info)
	return statInfo{
		Name:        e.Name,
		Size:        e.Size,
		SizeUnknown: e.SizeUnknown,
		IsFolder:    e.IsFolder,
		Modified:    e.Modified,
	}, nil
}

// sanitizeLocalName makes a device filename safe to use on the local
// filesystem. Device filenames are attacker-adjacent input: a name containing
// a path separator or "..", written verbatim, would let a download escape the
// destination directory.
func sanitizeLocalName(name string) string {
	name = strings.ReplaceAll(name, "\x00", "")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	name = strings.TrimSpace(name)
	switch name {
	case "", ".", "..":
		return "_"
	}
	if strings.HasPrefix(name, "..") {
		name = "_" + name
	}
	return name
}
