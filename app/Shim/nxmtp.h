// Declarations for the exports of nxmtp.dylib (built from backend/ffi).
//
// This mirrors the header cgo generates at backend/build/nxmtp.h, minus the
// cgo boilerplate, so the Xcode project can compile without the Go build
// having run first. scripts/build-backend.sh verifies that every symbol named
// here is actually exported by the dylib, which catches drift.
#pragma once

#ifdef __cplusplus
extern "C" {
#endif

typedef void (*on_cb_result_t)(char *);

extern void FetchAvailableDevices(on_cb_result_t onDone);
extern void Initialize(char *initInputJson, on_cb_result_t onDone);
extern void FetchDeviceInfo(char *deviceInputJson, on_cb_result_t onDone);
extern void FetchStorages(char *deviceInputJson, on_cb_result_t onDone);
extern void Walk(char *walkInputJson, on_cb_result_t onDone);
extern void MakeDirectory(char *makeDirectoryInputJson, on_cb_result_t onDone);
extern void RenameFile(char *renameFileInputJson, on_cb_result_t onDone);
extern void DeleteFile(char *deleteFileInputJson, on_cb_result_t onDone);
extern void FileExists(char *fileExistsInputJson, on_cb_result_t onDone);
extern void DownloadFiles(char *downloadFilesInputJson,
                          on_cb_result_t onPreprocess,
                          on_cb_result_t onProgress,
                          on_cb_result_t onDone);
extern void UploadFiles(char *uploadFilesInputJson,
                        on_cb_result_t onPreprocess,
                        on_cb_result_t onProgress,
                        on_cb_result_t onDone);
extern void CancelTransfer(char *cancelTransferInputJson, on_cb_result_t onDone);
extern void Dispose(char *deviceInputJson, on_cb_result_t onDone);
extern void FetchDiagnostics(on_cb_result_t onDone);
extern void SetVerboseLogging(int enabled);

#ifdef __cplusplus
} // extern "C"
#endif
