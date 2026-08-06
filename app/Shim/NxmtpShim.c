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

#include "NxmtpShim.h"

#include "nxmtp.h"

// The backend takes `char *` because cgo does not emit const-qualified
// parameters. It only ever reads the buffer, copying it before returning, so
// casting away const here is safe.
#define NX_IN(s) ((char *)(s))

void NxmtpFetchAvailableDevices(NxmtpOnCbResult onDone) {
  FetchAvailableDevices((on_cb_result_t)onDone);
}

void NxmtpInitialize(const char *initInputJson, NxmtpOnCbResult onDone) {
  Initialize(NX_IN(initInputJson), (on_cb_result_t)onDone);
}

void NxmtpFetchDeviceInfo(const char *deviceInputJson, NxmtpOnCbResult onDone) {
  FetchDeviceInfo(NX_IN(deviceInputJson), (on_cb_result_t)onDone);
}

void NxmtpFetchStorages(const char *deviceInputJson, NxmtpOnCbResult onDone) {
  FetchStorages(NX_IN(deviceInputJson), (on_cb_result_t)onDone);
}

void NxmtpWalk(const char *walkInputJson, NxmtpOnCbResult onDone) {
  Walk(NX_IN(walkInputJson), (on_cb_result_t)onDone);
}

void NxmtpMakeDirectory(const char *makeDirectoryInputJson, NxmtpOnCbResult onDone) {
  MakeDirectory(NX_IN(makeDirectoryInputJson), (on_cb_result_t)onDone);
}

void NxmtpFileExists(const char *fileExistsInputJson, NxmtpOnCbResult onDone) {
  FileExists(NX_IN(fileExistsInputJson), (on_cb_result_t)onDone);
}

void NxmtpDeleteFile(const char *deleteFileInputJson, NxmtpOnCbResult onDone) {
  DeleteFile(NX_IN(deleteFileInputJson), (on_cb_result_t)onDone);
}

void NxmtpRenameFile(const char *renameFileInputJson, NxmtpOnCbResult onDone) {
  RenameFile(NX_IN(renameFileInputJson), (on_cb_result_t)onDone);
}

void NxmtpUploadFiles(const char *uploadFilesInputJson,
                      NxmtpOnCbResult onPreprocess,
                      NxmtpOnCbResult onProgress,
                      NxmtpOnCbResult onDone) {
  UploadFiles(NX_IN(uploadFilesInputJson),
              (on_cb_result_t)onPreprocess,
              (on_cb_result_t)onProgress,
              (on_cb_result_t)onDone);
}

void NxmtpDownloadFiles(const char *downloadFilesInputJson,
                        NxmtpOnCbResult onPreprocess,
                        NxmtpOnCbResult onProgress,
                        NxmtpOnCbResult onDone) {
  DownloadFiles(NX_IN(downloadFilesInputJson),
                (on_cb_result_t)onPreprocess,
                (on_cb_result_t)onProgress,
                (on_cb_result_t)onDone);
}

void NxmtpCancelTransfer(const char *cancelTransferInputJson, NxmtpOnCbResult onDone) {
  CancelTransfer(NX_IN(cancelTransferInputJson), (on_cb_result_t)onDone);
}

void NxmtpDispose(const char *deviceInputJson, NxmtpOnCbResult onDone) {
  Dispose(NX_IN(deviceInputJson), (on_cb_result_t)onDone);
}

void NxmtpFetchDiagnostics(NxmtpOnCbResult onDone) {
  FetchDiagnostics((on_cb_result_t)onDone);
}

void NxmtpSetVerboseLogging(int enabled) { SetVerboseLogging(enabled); }
