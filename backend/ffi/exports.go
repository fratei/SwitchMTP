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

// Command nxmtp is the Go side of SwitchMTP: a c-shared library exposing MTP
// operations to the macOS app and CLI over a JSON-in / JSON-out C interface.
//
// Threading contract: every export runs synchronously on the calling thread
// and invokes its callbacks before returning. The Swift client calls these
// from a background dispatch queue, and passes C string pointers that are only
// valid for the duration of the call -- so every input string is copied
// immediately via C.GoString.
//
// The one exception is CancelTransfer, which is expected to be called from a
// different thread while a transfer is in progress.
package main

/*
#include <stdlib.h>

typedef void (*on_cb_result_t)(char*);

// invoke_cb exists because cgo cannot call a C function pointer directly from
// Go. Calling through this trampoline is the supported pattern.
static void invoke_cb(on_cb_result_t cb, char* payload) {
	if (cb != NULL) {
		cb(payload);
	}
}
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	"github.com/fratei/SwitchMTP/backend/ffi/result"
	"github.com/fratei/SwitchMTP/backend/nxmtp"
)

func main() {}

// --- callback plumbing --------------------------------------------------

// emit delivers a JSON payload to a C callback.
//
// The buffer is allocated with C.CString and freed as soon as the callback
// returns. The Swift side copies what it needs synchronously, which the
// existing client already does.
func emit(cb C.on_cb_result_t, payload string) {
	if cb == nil {
		return
	}
	c := C.CString(payload)
	defer C.free(unsafe.Pointer(c))
	C.invoke_cb(cb, c)
}

func emitSuccess(cb C.on_cb_result_t, data interface{}) {
	emit(cb, result.Success(data))
}

// emitError converts a Go error into the envelope's errorType/error/hint.
func emitError(cb C.on_cb_result_t, err error) {
	kind := string(nxmtp.KindOf(err))
	if kind == "" {
		kind = "unknown"
	}
	emit(cb, result.Failure(kind, err.Error(), hintOf(err)))
}

func hintOf(err error) string {
	if e, ok := err.(*nxmtp.Error); ok {
		return e.Hint
	}
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if e, ok := err.(*nxmtp.Error); ok {
			return e.Hint
		}
		u, ok := err.(unwrapper)
		if !ok {
			return ""
		}
		err = u.Unwrap()
	}
	return ""
}

// decodeInput copies and parses an input JSON string.
func decodeInput(raw *C.char, out interface{}) error {
	s := C.GoString(raw)
	if s == "" {
		return &nxmtp.Error{Kind: nxmtp.KindInvalidInput, Op: "input", Msg: "empty input"}
	}
	if err := json.Unmarshal([]byte(s), out); err != nil {
		return &nxmtp.Error{Kind: nxmtp.KindInvalidInput, Op: "input",
			Msg: "malformed input JSON", Err: err}
	}
	return nil
}

// --- input shapes -------------------------------------------------------
//
// Field names are fixed by the existing Swift client.

type deviceInput struct {
	DeviceID string `json:"deviceId"`
}

type walkInput struct {
	DeviceID            string `json:"deviceId"`
	StorageID           uint32 `json:"storageId"`
	FullPath            string `json:"fullPath"`
	Recursive           bool   `json:"recursive"`
	SkipDisallowedFiles bool   `json:"skipDisallowedFiles"`
	SkipHiddenFiles     bool   `json:"skipHiddenFiles"`
}

type transferInput struct {
	DeviceID        string   `json:"deviceId"`
	StorageID       uint32   `json:"storageId"`
	Sources         []string `json:"sources"`
	Destination     string   `json:"destination"`
	PreprocessFiles bool     `json:"preprocessFiles"`
}

type makeDirectoryInput struct {
	DeviceID  string `json:"deviceId"`
	StorageID uint32 `json:"storageId"`
	FullPath  string `json:"fullPath"`
}

type renameInput struct {
	DeviceID    string `json:"deviceId"`
	StorageID   uint32 `json:"storageId"`
	FullPath    string `json:"fullPath"`
	NewFileName string `json:"newFileName"`
}

type filesInput struct {
	DeviceID  string   `json:"deviceId"`
	StorageID uint32   `json:"storageId"`
	Files     []string `json:"files"`
}

// walkFile is a directory entry as sent to Swift. It exists separately from
// nxmtp.FileInfo purely so dateAdded uses the client's expected date format.
type walkFile struct {
	Size        int64       `json:"size"`
	IsFolder    bool        `json:"isFolder"`
	DateAdded   result.Time `json:"dateAdded"`
	Name        string      `json:"name"`
	Path        string      `json:"path"`
	ParentPath  string      `json:"parentPath"`
	Extension   string      `json:"extension"`
	ParentID    uint32      `json:"parentId"`
	ObjectID    uint32      `json:"objectId"`
	SizeUnknown bool        `json:"sizeUnknown"`
}

func toWalkFiles(in []nxmtp.FileInfo) []walkFile {
	out := make([]walkFile, len(in))
	for i, f := range in {
		out[i] = walkFile{
			Size:        f.Size,
			IsFolder:    f.IsFolder,
			DateAdded:   result.Time{Time: f.DateAdded},
			Name:        f.Name,
			Path:        f.Path,
			ParentPath:  f.ParentPath,
			Extension:   f.Extension,
			ParentID:    f.ParentID,
			ObjectID:    f.ObjectID,
			SizeUnknown: f.SizeUnknown,
		}
	}
	return out
}

// withClient resolves the device for an operation, reporting failures through
// the callback. A session that turns out to be dead is dropped so the next
// call reconnects instead of reusing a broken handle.
func withClient(deviceID string, cb C.on_cb_result_t, fn func(*nxmtp.Client) (interface{}, error)) {
	c, err := reg.require(deviceID)
	if err != nil {
		emitError(cb, err)
		return
	}
	data, err := fn(c)
	if err != nil {
		if nxmtp.IsDisconnected(err) {
			reg.drop(deviceID)
		}
		emitError(cb, err)
		return
	}
	emitSuccess(cb, data)
}

// --- exports ------------------------------------------------------------

//export FetchAvailableDevices
func FetchAvailableDevices(onDone C.on_cb_result_t) {
	devices, err := nxmtp.FindDevices()
	if err != nil {
		emitError(onDone, err)
		return
	}
	if devices == nil {
		devices = []nxmtp.DeviceRef{}
	}
	emitSuccess(onDone, devices)
}

//export Initialize
func Initialize(initInputJson *C.char, onDone C.on_cb_result_t) {
	var in deviceInput
	if err := decodeInput(initInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	c, err := reg.open(in.DeviceID)
	if err != nil {
		emitError(onDone, err)
		return
	}
	emitSuccess(onDone, c.Details())
}

//export FetchDeviceInfo
func FetchDeviceInfo(deviceInputJson *C.char, onDone C.on_cb_result_t) {
	var in deviceInput
	if err := decodeInput(deviceInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		return c.Details(), nil
	})
}

//export FetchStorages
func FetchStorages(deviceInputJson *C.char, onDone C.on_cb_result_t) {
	var in deviceInput
	if err := decodeInput(deviceInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		return c.Storages()
	})
}

//export Walk
func Walk(walkInputJson *C.char, onDone C.on_cb_result_t) {
	var in walkInput
	if err := decodeInput(walkInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		files, err := c.Walk(nxmtp.WalkOptions{
			StorageID:           in.StorageID,
			FullPath:            in.FullPath,
			Recursive:           in.Recursive,
			SkipHiddenFiles:     in.SkipHiddenFiles,
			SkipDisallowedFiles: in.SkipDisallowedFiles,
		})
		if err != nil {
			return nil, err
		}
		return toWalkFiles(files), nil
	})
}

//export MakeDirectory
func MakeDirectory(makeDirectoryInputJson *C.char, onDone C.on_cb_result_t) {
	var in makeDirectoryInput
	if err := decodeInput(makeDirectoryInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		return nil, c.MakeDirectory(in.StorageID, in.FullPath)
	})
}

//export RenameFile
func RenameFile(renameFileInputJson *C.char, onDone C.on_cb_result_t) {
	var in renameInput
	if err := decodeInput(renameFileInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		return nil, c.Rename(in.StorageID, in.FullPath, in.NewFileName)
	})
}

//export DeleteFile
func DeleteFile(deleteFileInputJson *C.char, onDone C.on_cb_result_t) {
	var in filesInput
	if err := decodeInput(deleteFileInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		return nil, c.Delete(in.StorageID, in.Files)
	})
}

//export FileExists
func FileExists(fileExistsInputJson *C.char, onDone C.on_cb_result_t) {
	var in filesInput
	if err := decodeInput(fileExistsInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		return c.FileExists(in.StorageID, in.Files)
	})
}

//export DownloadFiles
func DownloadFiles(downloadFilesInputJson *C.char, onPreprocess, onProgress, onDone C.on_cb_result_t) {
	var in transferInput
	if err := decodeInput(downloadFilesInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		return c.Download(
			nxmtp.DownloadRequest{
				StorageID:       in.StorageID,
				Sources:         in.Sources,
				Destination:     in.Destination,
				PreprocessFiles: in.PreprocessFiles,
			},
			func(p nxmtp.PreprocessResult) { emitSuccess(onPreprocess, p) },
			func(p nxmtp.Progress) { emitSuccess(onProgress, p) },
		)
	})
}

//export UploadFiles
func UploadFiles(uploadFilesInputJson *C.char, onPreprocess, onProgress, onDone C.on_cb_result_t) {
	var in transferInput
	if err := decodeInput(uploadFilesInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	withClient(in.DeviceID, onDone, func(c *nxmtp.Client) (interface{}, error) {
		return c.Upload(
			nxmtp.UploadRequest{
				StorageID:       in.StorageID,
				Sources:         in.Sources,
				Destination:     in.Destination,
				PreprocessFiles: in.PreprocessFiles,
			},
			func(p nxmtp.PreprocessResult) { emitSuccess(onPreprocess, p) },
			func(p nxmtp.Progress) { emitSuccess(onProgress, p) },
		)
	})
}

// CancelTransfer aborts the transfer in flight on a device.
//
// Unlike every other export this is expected to be called concurrently with a
// running operation, and so must not take any lock the transfer holds.
//
//export CancelTransfer
func CancelTransfer(cancelTransferInputJson *C.char, onDone C.on_cb_result_t) {
	var in deviceInput
	if err := decodeInput(cancelTransferInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	reg.cancel(in.DeviceID)
	emitSuccess(onDone, nil)
}

//export Dispose
func Dispose(deviceInputJson *C.char, onDone C.on_cb_result_t) {
	var in deviceInput
	if err := decodeInput(deviceInputJson, &in); err != nil {
		emitError(onDone, err)
		return
	}
	if in.DeviceID == "" {
		reg.closeAll()
	} else {
		reg.close(in.DeviceID)
	}
	emitSuccess(onDone, nil)
}

// FetchDiagnostics reports why a device may not be usable: which processes
// hold USB clients, what USB hardware is visible, and whether a Switch is
// present but in the wrong mode.
//
// It deliberately does not require an open session -- its whole purpose is to
// explain a failure to connect.
//
//export FetchDiagnostics
func FetchDiagnostics(onDone C.on_cb_result_t) {
	emitSuccess(onDone, nxmtp.CollectDiagnostics())
}

// SetVerboseLogging toggles protocol logging at runtime.
//
//export SetVerboseLogging
func SetVerboseLogging(enabled C.int) {
	nxmtp.SetVerbose(enabled != 0)
}
