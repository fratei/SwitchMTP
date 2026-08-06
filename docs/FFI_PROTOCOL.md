# FFI Protocol: Swift ↔ Go JSON Contract

This document describes the C-function / JSON interface between the macOS Swift app and
`nxmtp.dylib` (the Go backend). The contract must be preserved exactly; the Swift shim
(`app/Shim/NxmtpShim.{c,h}`) depends on it.

---

## Response envelope

Every callback from Go to Swift carries the same JSON envelope:

```json
{
  "errorType": "",
  "error":     "",
  "data":      <any>
}
```

| Field | Type | Meaning |
|---|---|---|
| `errorType` | `string` | Empty string on success. Error class on failure — see [Error types](#error-types). |
| `error` | `string` | Human-readable error message. Empty on success. |
| `data` | any | Operation-specific payload. `null` for operations that produce no output. |

---

## Error types

| `errorType` value | Meaning |
|---|---|
| `""` (empty) | Success |
| `"DeviceDisconnected"` | Device unplugged or USB session lost |
| `"PermissionDenied"` | `libusb_claim_interface` failed — interface occupied |
| `"OperationUnsupported"` | Device returned MTP `OperationNotSupported` |
| `"Cancelled"` | Transfer was cancelled via `CancelTransfer` |
| `"UnknownError"` | Any other error |

---

## `deviceId` format

Every input JSON (except `FetchAvailableDevices`) carries a `deviceId` field that
uniquely identifies the connected device for the lifetime of the OS session.

**Format:** `"<vendorId>|<productId>|<serialNumber>"`

- `vendorId` and `productId` are **decimal** integers (not hex).
- `serialNumber` is the USB string descriptor value (e.g. `XAW10000000000`).

**Example:** `"1406|8221|XAW10000000000"`
(0x057E = 1406, 0x201D = 8221)

---

## Date format

All date/time strings use: `2006-01-02T15:04:05.000Z` (Go reference time, UTC, millisecond precision).

---

## Exports

### `FetchAvailableDevices`

Lists all connected MTP-capable devices.

**Input:** *(none)*

**`data` payload:** array of device descriptors

```json
[
  {
    "vendorId":      1406,
    "productId":     8221,
    "manufacturer":  "Nintendo Co., Ltd.",
    "model":         "NX",
    "serialNumber":  "XAW10000000000"
  }
]
```

**SwitchMTP extension:** none on this export.

---

### `Initialize`

Opens an MTP session with the device and reads its info. Must be called before any
other per-device export.

**Input:**

| Key | Type | Required |
|---|---|---|
| `deviceId` | string | yes |

**`data` payload:**

```json
{
  "mtpDeviceInfo": {
    "Manufacturer":        "Nintendo Co., Ltd.",
    "Model":               "NX",
    "DeviceVersion":       "...",
    "SerialNumber":        "XAW10000000000",
    "MTPExtension":        "...",
    "OperationsSupported": [4096, 4097, ...]
  },
  "usbDeviceInfo": {
    "DeviceName":    "NX",
    "Manufacturer":  "Nintendo Co., Ltd.",
    "SerialNumber":  "XAW10000000000"
  }
}
```

**SwitchMTP extensions (additive):**

| Key | Type | Meaning |
|---|---|---|
| `capabilities` | object | Boolean flags for each supported MTP operation, derived from `OperationsSupported`. Keys include `getObjectPropList`, `getObjectPropValue`, `setObjectPropValue`, `getPartialObject`, `moveObject`, `copyObject`, `getNumObjects`. |
| `deviceProfile` | string | `"switchDBI"` / `"switchHOS"` / `"generic"` — Switch MTP at 0x201D is classified further by querying device info. |

---

### `FetchDeviceInfo`

Re-reads device info without re-opening the session. Returns the same shape as
`Initialize`.

**Input:** `deviceId`

---

### `FetchStorages`

Lists all MTP storages on the device.

**Input:** `deviceId`

**`data` payload:** array of storage descriptors

```json
[
  {
    "Sid": 65537,
    "Info": {
      "StorageDescription":  "SD Card",
      "MaxCapability":       128000000000,
      "FreeSpaceInBytes":    64000000000,
      "AccessCapability":    0,
      "StorageType":         4,
      "FilesystemType":      2
    }
  }
]
```

**SwitchMTP extensions (additive):**

| Key | Type | Meaning |
|---|---|---|
| `kind` | string | Storage classification. One of: `sdCard` \| `nandUser` \| `nandSystem` \| `installedGames` \| `sdInstall` \| `nandInstall` \| `saves` \| `album` \| `gamecard` \| `custom` \| `unknown` |
| `capabilities` | string (comma-separated flags) | Subset of: `browse`, `read`, `write`, `delete`, `rename`, `installTarget` |

---

### `Walk`

Lists objects under a path, optionally recursive.

**Input:**

| Key | Type | Required | Notes |
|---|---|---|---|
| `deviceId` | string | yes | |
| `storageId` | number | yes | |
| `fullPath` | string | yes | `"/"` for root |
| `recursive` | bool | no | Default `false` |
| `skipDisallowedFiles` | bool | no | |
| `skipHiddenFiles` | bool | no | |

**`data` payload:** array of file/directory entries

```json
[
  {
    "size":        1234567,
    "isFolder":    false,
    "dateAdded":   "2024-03-15T10:22:00.000Z",
    "name":        "example.nsp",
    "path":        "/example.nsp",
    "parentPath":  "/",
    "extension":   "nsp",
    "parentId":    4294967295,
    "objectId":    12345
  }
]
```

**SwitchMTP extension (additive):**

| Key | Type | Meaning |
|---|---|---|
| `sizeUnknown` | bool | `true` when `ObjectInfo.CompressedSize == 0xFFFFFFFF` and `GetObjectPropValue(OPC_ObjectSize)` was either unsupported or returned an unreliable value. When `true`, the UI must display `—` rather than a size. |

---

### `UploadFiles`

Uploads files from the Mac to the device.

**Input:**

| Key | Type | Notes |
|---|---|---|
| `deviceId` | string | |
| `storageId` | number | |
| `sources` | string array | Local file paths |
| `destination` | string | Target path on device |
| `preprocessFiles` | bool | |

**Progress callbacks:** three separate callbacks are fired during the operation:
`queued` → `progress` → `done`. Each carries the progress payload (see below).

---

### `DownloadFiles`

Downloads files from the device to the Mac.

**Input:**

| Key | Type | Notes |
|---|---|---|
| `deviceId` | string | |
| `storageId` | number | |
| `sources` | string array | Device paths |
| `destination` | string | Local destination directory |
| `preprocessFiles` | bool | |

**Progress callbacks:** same shape as `UploadFiles`.

---

### `MakeDirectory`

Creates a directory.

**Input:** `deviceId`, `storageId`, `fullPath`

**`data`:** `null`

---

### `RenameFile`

Renames a file or directory (calls `SetObjectPropValue` for `OPC_ObjectFileName`).
Will fail with `OperationUnsupported` if the device did not advertise
`SetObjectPropValue`.

**Input:** `deviceId`, `storageId`, `fullPath`, `newFileName`

**`data`:** `null`

---

### `DeleteFile`

Deletes one or more files or directories.

**Input:**

| Key | Type |
|---|---|
| `deviceId` | string |
| `storageId` | number |
| `files` | string array (device paths) |

**`data`:** `null`

---

### `FileExists`

Checks whether paths exist on the device.

**Input:** `deviceId`, `storageId`, `files` (string array)

**`data` payload:**

```json
[
  { "fullpath": "/example.nsp", "exists": true }
]
```

---

### `CancelTransfer`

Cancels the in-flight transfer for the specified device. The cancelled upload
best-effort deletes any partial object already written to the device.

**Input:** `deviceId`

**`data`:** `null`

---

### `Dispose`

Closes the MTP session and releases all resources for the device. Must be called on
disconnect or app termination.

**Input:** `deviceId`

**`data`:** `null`

---

### `FetchDiagnostics` *(SwitchMTP addition)*

Returns a diagnostics bundle for the troubleshooting UI and bug reports. Does not
require an open session — it operates at the USB enumeration level.

**Input:** `deviceId` *(optional — if omitted, reports all connected USB devices)*

**`data` payload:**

```json
{
  "usbDevices": [
    {
      "vendorId": 1406,
      "productId": 8221,
      "manufacturer": "Nintendo Co., Ltd.",
      "model": "NX",
      "serialNumber": "XAW10000000000",
      "speed": "SUPER",
      "occupyingPid": 1234,
      "occupyingProcess": "com.apple.ImageCaptureExtension2",
      "occupyingGuidance": "Quit Image Capture Extension: kill 1234"
    }
  ],
  "deviceInfo": { ... },
  "sessionOpen": true
}
```

---

## Progress payload

`UploadFiles` and `DownloadFiles` fire callbacks with this shape in `data`:

```json
{
  "fullPath":          "/path/to/file.nsp",
  "name":              "file.nsp",
  "elapsedTime":       12.4,
  "speed":             15728640,
  "totalFiles":        3,
  "totalDirectories":  0,
  "filesSent":         1,
  "filesSentProgress": 0.5,
  "activeFileSize": {
    "total":    1073741824,
    "sent":     536870912,
    "progress": 0.5
  },
  "bulkFileSize": {
    "total":    3221225472,
    "sent":     536870912,
    "progress": 0.166
  },
  "status": "inProgress"
}
```

| `status` value | Meaning |
|---|---|
| `"queued"` | Transfer is queued, not yet started |
| `"inProgress"` | Transfer is active |
| `"done"` | Transfer completed successfully |
| `"error"` | Transfer failed |
| `"cancelled"` | Transfer was cancelled |
