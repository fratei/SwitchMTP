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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// UploadRequest describes an upload from the local disk to the device.
type UploadRequest struct {
	StorageID       uint32
	Sources         []string
	Destination     string
	PreprocessFiles bool
}

// uploadItem is one local file scheduled for upload.
type uploadItem struct {
	localPath  string
	devicePath string
	size       int64
	modified   time.Time
}

// Upload copies local files and directories to the device.
//
// When the target storage is one of DBI's install storages the behaviour
// changes substantially -- see uploadInstall.
func (c *Client) Upload(req UploadRequest, onPreprocess func(PreprocessResult), onProgress ProgressFunc) (*TransferSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx := c.beginCancellable()
	defer c.endCancellable()
	_ = ctx

	st, err := c.storageByID(req.StorageID)
	if err != nil {
		return nil, err
	}
	if !st.Capabilities.Write {
		return nil, &Error{
			Kind: KindReadOnly,
			Op:   "upload",
			Msg:  "storage \"" + st.DisplayName + "\" is read-only",
		}
	}

	items, dirs, err := planUpload(req)
	if err != nil {
		return nil, err
	}

	var totalBytes int64
	for _, it := range items {
		totalBytes += it.size
	}

	if onPreprocess != nil {
		onPreprocess(PreprocessResult{
			TotalFiles:       int64(len(items)),
			TotalDirectories: int64(dirs),
			TotalSize:        totalBytes,
		})
	}

	if st.Capabilities.InstallTarget {
		return c.uploadInstall(st, items, totalBytes, onProgress)
	}

	tracker := newProgressTracker(onProgress)
	tracker.setTotals(int64(len(items)), int64(dirs), totalBytes, false)

	summary := &TransferSummary{
		TotalFiles:       int64(len(items)),
		TotalDirectories: int64(dirs),
	}

	for _, it := range items {
		if err := c.checkCancelled(); err != nil {
			tracker.setStatus(StatusCancelled, "")
			return nil, err
		}
		n, err := c.uploadOne(req.StorageID, it, tracker)
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

// uploadInstall handles DBI's write-only install storages.
//
// These behave unlike any ordinary storage:
//   - Only NSP/NSZ/XCI/XCZ are accepted; anything else is rejected up front
//     rather than being written and silently ignored.
//   - Files go to the storage root; subdirectories are meaningless.
//   - The installation runs on the console *after* the bytes arrive, and MTP
//     provides no completion event. We therefore report an "installing" state
//     and tell the caller to watch the Switch screen.
//   - Installs are serialised. Sending a second file while the console is
//     still committing the first is the fastest way to wedge DBI.
func (c *Client) uploadInstall(st *Storage, items []uploadItem, totalBytes int64, onProgress ProgressFunc) (*TransferSummary, error) {
	var accepted []uploadItem
	var skipped []string
	for _, it := range items {
		if IsInstallable(filepath.Base(it.localPath)) {
			accepted = append(accepted, it)
		} else {
			skipped = append(skipped, filepath.Base(it.localPath))
		}
	}

	if len(accepted) == 0 {
		return nil, &Error{
			Kind: KindInvalidInput,
			Op:   "install",
			Msg:  "no installable files selected",
			Hint: "\"" + st.DisplayName + "\" only accepts NSP, NSZ, XCI and XCZ files.",
		}
	}

	var acceptedBytes int64
	for _, it := range accepted {
		acceptedBytes += it.size
	}

	tracker := newProgressTracker(onProgress)
	tracker.setTotals(int64(len(accepted)), 0, acceptedBytes, false)

	summary := &TransferSummary{TotalFiles: int64(len(accepted)), Skipped: skipped}

	for i, it := range accepted {
		if err := c.checkCancelled(); err != nil {
			tracker.setStatus(StatusCancelled, "")
			return nil, err
		}

		// Install storages are flat: force the destination to the root.
		it.devicePath = "/" + sanitizeLocalName(filepath.Base(it.localPath))

		n, err := c.uploadOne(st.Sid, it, tracker)
		if err != nil {
			tracker.setStatus(StatusFailed, "")
			return nil, annotateInstallError(err, st)
		}
		summary.TotalBytes += n

		tracker.setStatus(StatusInstalling,
			"The Switch is installing "+filepath.Base(it.localPath)+". Progress is shown on the console.")

		// Wait for the console to become responsive again before sending the
		// next title. There is no completion event, so the proxy for "the
		// install finished" is the device answering a cheap query again.
		if i < len(accepted)-1 {
			if err := c.waitForDeviceReady(); err != nil {
				return nil, err
			}
		}
	}

	tracker.setStatus(StatusCompleted, "Transfer complete. Installation continues on the Switch.")
	summary.Elapsed = now().Sub(tracker.start).Seconds()
	summary.Note = "SwitchMTP has sent the files. DBI reports installation progress on the console; " +
		"MTP provides no completion signal, so check the Switch screen."
	c.cache.invalidateStorage(st.Sid)
	return summary, nil
}

// waitForDeviceReady polls a cheap operation until the responder answers,
// which indicates it has finished committing the previous install.
func (c *Client) waitForDeviceReady() error {
	const (
		attempts = 120
		delay    = time.Second
	)
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := c.checkCancelled(); err != nil {
			return err
		}
		var ids mtp.Uint32Array
		if err := c.t.GetStorageIDs(&ids); err == nil {
			return nil
		} else {
			lastErr = err
			if IsDisconnected(err) {
				return classify("install", err)
			}
		}
		time.Sleep(delay)
	}
	return &Error{
		Kind: KindDeviceBusy,
		Op:   "install",
		Msg:  "the Switch did not become ready again after the previous installation",
		Hint: "Check the console for an error. Large installs can fail in applet mode -- " +
			"launch DBI by holding R while starting a game.",
		Err: lastErr,
	}
}

// annotateInstallError attaches DBI-specific advice to a failed install.
func annotateInstallError(err error, st *Storage) error {
	var e *Error
	if ok := asError(err, &e); !ok {
		return err
	}
	if e.Hint != "" {
		return e
	}
	switch e.Kind {
	case KindStorageFull:
		e.Hint = "Not enough free space on " + st.DisplayName + "."
	case KindDeviceBusy, KindUnknown:
		e.Hint = "If the Switch reported \"Extra buffers exceeded. Media write speed is too low\", " +
			"DBI is running in applet mode. Quit DBI and relaunch it by holding R while starting an " +
			"installed game, then try again."
	}
	return e
}

// planUpload expands local sources into a flat file list.
func planUpload(req UploadRequest) ([]uploadItem, int, error) {
	dest := NormalizePath(req.Destination)
	var items []uploadItem
	dirs := 0

	for _, src := range req.Sources {
		info, err := os.Stat(src)
		if err != nil {
			return nil, 0, wrapErr(KindLocalIO, "upload", err)
		}

		if !info.IsDir() {
			items = append(items, uploadItem{
				localPath:  src,
				devicePath: JoinPath(dest, info.Name()),
				size:       info.Size(),
				modified:   info.ModTime(),
			})
			continue
		}

		root := JoinPath(dest, info.Name())
		dirs++
		err = filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(src, p)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			devicePath := JoinPath(root, filepath.ToSlash(rel))
			if fi.IsDir() {
				dirs++
				return nil
			}
			if !fi.Mode().IsRegular() {
				// Symlinks, sockets and devices have no MTP representation.
				return nil
			}
			items = append(items, uploadItem{
				localPath:  p,
				devicePath: devicePath,
				size:       fi.Size(),
				modified:   fi.ModTime(),
			})
			return nil
		})
		if err != nil {
			return nil, 0, wrapErr(KindLocalIO, "upload", err)
		}
	}
	return items, dirs, nil
}

// uploadOne sends a single file, creating parent directories as needed.
func (c *Client) uploadOne(storageID uint32, it uploadItem, tracker *progressTracker) (int64, error) {
	name := filepath.Base(it.localPath)
	tracker.beginFile(name, it.devicePath, it.size)

	f, err := os.Open(it.localPath)
	if err != nil {
		return 0, wrapErr(KindLocalIO, "upload", err)
	}
	defer f.Close()

	parentDir := NormalizePath(pathDir(it.devicePath))
	parent, err := c.ensureDirectory(storageID, parentDir)
	if err != nil {
		return 0, err
	}

	deviceName := deviceSafeName(pathBase(it.devicePath))

	// Replace an existing object rather than creating a duplicate: MTP happily
	// allows two entries with the same name in one directory, which then
	// becomes impossible to disambiguate.
	if existing, err := c.findChild(storageID, parent, deviceName); err == nil {
		if c.caps.DeleteObject {
			if err := c.t.DeleteObject(existing.handle); err != nil && !IsUnsupported(err) {
				return 0, classify("upload", err)
			}
			c.cache.invalidate(storageID, it.devicePath)
		}
	}

	info := &mtp.ObjectInfo{
		StorageID:        storageID,
		ObjectFormat:     formatForName(deviceName),
		ParentObject:     parent,
		Filename:         deviceName,
		CompressedSize:   clampSize(it.size),
		ModificationDate: it.modified,
		CaptureDate:      it.modified,
	}

	_, _, handle, err := c.t.SendObjectInfo(storageID, parent, info)
	if err != nil {
		return 0, classify("upload "+it.devicePath, err)
	}

	var sent int64
	progress := func(n int64) error {
		sent = n
		if err := c.checkCancelled(); err != nil {
			return err
		}
		tracker.advance(n)
		return nil
	}

	if err := c.t.SendObject(f, it.size, progress); err != nil {
		// A partially-sent object is garbage on the device; remove it so the
		// user is not left with a truncated file that looks real.
		if c.caps.DeleteObject {
			_ = c.t.DeleteObject(handle)
		}
		if KindOf(err) == KindCancelled {
			return 0, err
		}
		return 0, classify("upload "+it.devicePath, err)
	}

	c.cache.put(storageID, NormalizePath(it.devicePath), cachedNode{handle: handle, isFolder: false})
	tracker.endFile()
	return sent, nil
}

// ensureDirectory resolves a directory path, creating any missing components.
func (c *Client) ensureDirectory(storageID uint32, dir string) (uint32, error) {
	dir = NormalizePath(dir)
	if dir == "/" {
		return ParentRoot, nil
	}
	if node, err := c.resolvePath(storageID, dir); err == nil {
		if !node.isFolder {
			return 0, errf(KindInvalidInput, "upload", "%q is not a directory", dir)
		}
		return node.handle, nil
	}

	parent, err := c.ensureDirectory(storageID, pathDir(dir))
	if err != nil {
		return 0, err
	}
	name := deviceSafeName(pathBase(dir))

	if node, err := c.findChild(storageID, parent, name); err == nil {
		c.cache.put(storageID, dir, node)
		return node.handle, nil
	}

	handle, err := c.makeDirectoryAt(storageID, parent, name)
	if err != nil {
		return 0, err
	}
	c.cache.put(storageID, dir, cachedNode{handle: handle, isFolder: true})
	return handle, nil
}

// clampSize fits a 64-bit size into ObjectInfo's 32-bit field. Objects larger
// than 4 GiB are announced as 0xFFFFFFFF, which is the conventional signal
// that the real size does not fit; DBI splits such files on arrival.
func clampSize(n int64) uint32 {
	if n < 0 {
		return 0
	}
	if n >= int64(sizeOverflow) {
		return sizeOverflow
	}
	return uint32(n)
}

// formatForName picks an MTP object format code from a filename. Responders
// mostly ignore this for ordinary files, but sending something coherent avoids
// upsetting stricter implementations.
func formatForName(name string) uint16 {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return mtp.OFC_EXIF_JPEG
	case ".png":
		return mtp.OFC_PNG
	case ".gif":
		return mtp.OFC_GIF
	case ".bmp":
		return mtp.OFC_BMP
	case ".txt":
		return mtp.OFC_Text
	case ".mp4":
		return mtp.OFC_Undefined
	default:
		return mtp.OFC_Undefined
	}
}

// deviceSafeName strips characters that FAT32/exFAT cannot store. Writing a
// name the filesystem rejects produces a confusing generic MTP error.
func deviceSafeName(name string) string {
	replacer := strings.NewReplacer(
		"\x00", "", "/", "_", "\\", "_", ":", "_",
		"*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	out := strings.TrimSpace(replacer.Replace(name))
	// Trailing dots and spaces are silently dropped by FAT, which then makes
	// the file impossible to address by the name we think it has.
	out = strings.TrimRight(out, ". ")
	if out == "" {
		return "_"
	}
	return out
}

func pathDir(p string) string {
	p = NormalizePath(p)
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

func pathBase(p string) string {
	p = NormalizePath(p)
	i := strings.LastIndex(p, "/")
	return p[i+1:]
}

// asError is errors.As specialised for *Error, kept local to avoid importing
// errors in files that do not otherwise need it.
func asError(err error, target **Error) bool {
	for err != nil {
		if e, ok := err.(*Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

var _ = io.Discard
