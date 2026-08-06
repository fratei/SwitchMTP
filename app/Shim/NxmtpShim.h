// SwitchMTP — a macOS MTP client for Nintendo Switch running DBI.
// Copyright (C) 2024 Neighbor_Z
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

// C entry points the Swift app calls into.
//
// These are thin wrappers over the nxmtp.dylib exports. The wrapper layer
// exists so Swift sees a stable, prefixed API and a Swift-friendly function
// pointer type, rather than the raw cgo-generated symbols.
//
// Threading: every call runs synchronously on the calling thread and invokes
// its callbacks before returning, so callers must dispatch to a background
// queue. NxmtpCancelTransfer is the exception -- it is designed to be called
// from another thread while a transfer is running.
//
// Lifetime: input strings are copied by the backend before it returns, so a
// scoped pointer from `withCString` is safe. Callback payloads are only valid
// for the duration of the callback and must be copied.
#pragma once

#ifdef __cplusplus
extern "C" {
#endif

/// Receives a JSON envelope: `{ "errorType": …, "error": …, "data": … }`.
typedef void (*NxmtpOnCbResult)(char *);

void NxmtpFetchAvailableDevices(NxmtpOnCbResult onDone);
void NxmtpInitialize(const char *initInputJson, NxmtpOnCbResult onDone);
void NxmtpFetchDeviceInfo(const char *deviceInputJson, NxmtpOnCbResult onDone);
void NxmtpFetchStorages(const char *deviceInputJson, NxmtpOnCbResult onDone);
void NxmtpWalk(const char *walkInputJson, NxmtpOnCbResult onDone);
void NxmtpMakeDirectory(const char *makeDirectoryInputJson, NxmtpOnCbResult onDone);
void NxmtpFileExists(const char *fileExistsInputJson, NxmtpOnCbResult onDone);
void NxmtpDeleteFile(const char *deleteFileInputJson, NxmtpOnCbResult onDone);
void NxmtpRenameFile(const char *renameFileInputJson, NxmtpOnCbResult onDone);
void NxmtpUploadFiles(const char *uploadFilesInputJson,
                      NxmtpOnCbResult onPreprocess,
                      NxmtpOnCbResult onProgress,
                      NxmtpOnCbResult onDone);
void NxmtpDownloadFiles(const char *downloadFilesInputJson,
                        NxmtpOnCbResult onPreprocess,
                        NxmtpOnCbResult onProgress,
                        NxmtpOnCbResult onDone);
void NxmtpCancelTransfer(const char *cancelTransferInputJson, NxmtpOnCbResult onDone);
void NxmtpDispose(const char *deviceInputJson, NxmtpOnCbResult onDone);

/// Reports USB state and any process holding the device, for troubleshooting.
/// Does not require an open session.
void NxmtpFetchDiagnostics(NxmtpOnCbResult onDone);

/// Enables protocol-level logging to stderr.
void NxmtpSetVerboseLogging(int enabled);

#ifdef __cplusplus
} // extern "C"
#endif
