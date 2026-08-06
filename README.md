# SwitchMTP

A native macOS app for browsing, managing, and transferring files between a Mac and a
**Nintendo Switch running DBI's MTP responder**. It lets you manage your own SD card
files, back up and restore save data, copy screenshots and videos to your Mac, send
NSP/NSZ/XCI/XCZ files to DBI's install targets, and dump your inserted game card — all
over a standard USB-C cable.

**Legitimate use only.** SwitchMTP is for managing files you own: your saves,
screenshots, homebrew, and personal backups of games you legally possess. The project has
no connection to piracy and ships no copyrighted content. The install flow is DBI's own
standard MTP mechanism.

---

## Requirements

- **macOS 12 (Monterey) or later**
- A Nintendo Switch (original / Lite / OLED) with **[DBI](https://github.com/rashevskyv/dbi)
  installed**
- A **USB-C data cable** — charge-only cables do not carry USB data and will not work

`ns-usbloader` is not required. It is a separate Java host app for the Tinfoil USB
protocol; SwitchMTP talks MTP to DBI directly and has no Java dependency.

---

## Install

1. Download the latest `SwitchMTP-x.y.z.dmg` from the
   [Releases](https://github.com/fratei/SwitchMTP/releases) page.
2. Open the DMG and drag `SwitchMTP.app` to `/Applications`.
3. **Remove the macOS quarantine attribute:**

   ```sh
   xattr -dr com.apple.quarantine /Applications/SwitchMTP.app
   ```

   **Why is this step needed?** SwitchMTP is signed with an ad-hoc certificate (not an
   Apple Developer Program certificate). macOS Gatekeeper quarantines apps downloaded from
   the internet that are not notarized by Apple. Running the command above tells macOS you
   have reviewed and trust this binary. You can also right-click
   `/Applications/SwitchMTP.app`, choose **Open**, then confirm **Open** when macOS asks. If
   you prefer neither path, **build from source instead** (see below) — a locally built app
   is not quarantined.

---

## Quick Start

1. On your Switch, open DBI and press **X → "Run MTP responder"**.
2. Connect the Switch to your Mac with a USB-C data cable.
3. SwitchMTP should detect the Switch and show its storages in the sidebar.

> **Trouble connecting?** See [`docs/DBI_SETUP.md`](docs/DBI_SETUP.md) for a step-by-step
> guide, and [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md) for common problems.
> Hardware notes live in [`docs/HARDWARE_VALIDATION.md`](docs/HARDWARE_VALIDATION.md).

---

## Features

- Browse DBI MTP storages that advertise browsing support (SD Card, Saves, Album,
  Installed Games, Game Card, custom storages, and more)
- Upload and download files with progress tracking and transfer cancellation
- Send `.nsp`, `.nsz`, `.xci`, and `.xcz` files to DBI's SD or NAND install targets
  (DBI reports install progress on the Switch screen; SwitchMTP can only report that
  the file was sent)
- Back up the Saves storage to a timestamped folder
- Restore Saves with a destructive confirmation when DBI exposes writable save access
- Export Album screenshots and videos to a timestamped folder
- Dump the inserted game card from DBI's virtual Game Card storage
- Rename, delete, and create directories only where the storage advertises those
  capabilities
- Detects when the Switch is in a non-MTP homebrew USB mode (`0x3000` — DBIbackend,
  Awoo-Installer or GoldLeaf) and tells you how to fix it
- Switch menu workflows: **Back Up Saves…**, **Restore Saves…**, **Export Album…**,
  **Dump Gamecard…**, **Install to SD Card…**, **Install to NAND…**, and
  **Copy Diagnostics**
- JSON diagnostics copied to the clipboard for bug reports
- Command-line interface (`switchmtp-cli`) bundled inside the app

---

## Building from Source

```sh
# 1. Verify prerequisites (Go ≥ 1.21, full Xcode, not just Command Line Tools)
scripts/bootstrap.sh

# 2. Build a universal libusb dylib into third_party/libusb/
scripts/build-libusb.sh

# 3. Build nxmtp.dylib (the Go backend)
scripts/build-backend.sh

# 4. Build the macOS app (xcodebuild, ad-hoc sign)
scripts/build-app.sh
```

A DMG suitable for distribution can be created with `scripts/package-dmg.sh`.

---

## Architecture

```
SwitchMTP.app (SwiftUI, GPL-2.0)
  └── NxmtpShim.c / .h  (C bridging shim)
        └── nxmtp.dylib  (Go, -buildmode=c-shared)
              ├── backend/ffi/        JSON envelope + multi-device registry
              └── backend/nxmtp/      high-level MTP layer (DBI-aware)
                    └── third_party/go-mtpfs/mtp   (low-level MTP protocol, New BSD)
                          └── third_party/usb/     (libusb cgo bindings, New BSD)
                                └── libusb-1.0.0.dylib  (LGPL-2.1, dynamically linked)
```

The Swift app communicates with the Go backend entirely through a JSON-over-C-function
API. See [`docs/FFI_PROTOCOL.md`](docs/FFI_PROTOCOL.md) for the full contract.

---

## Credits

- **[SwiftMTP](https://github.com/Neighbor-Z/SwiftMTP)** by Neighbor_Z — the SwiftUI macOS
  app that SwitchMTP is derived from (GPL-2.0).
- **[go-mtpfs](https://github.com/hanwen/go-mtpfs)** by Han-Wen Nienhuys / Google — the
  low-level MTP-over-libusb engine (New BSD).
- **[ganeshrvel/go-mtpfs](https://github.com/ganeshrvel/go-mtpfs)** and
  **[ganeshrvel/usb](https://github.com/ganeshrvel/usb)** by Ganesh Rathinavel — the forks
  with macOS diagnostics and speed fixes that we vendor (New BSD).
- **[DBI](https://github.com/rashevskyv/dbi)** by duckbill / rashevskyv — the Nintendo
  Switch MTP server that this app is designed to talk to. SwitchMTP is an unaffiliated
  client.

See [`THIRD_PARTY.md`](THIRD_PARTY.md) for full attribution and license texts.

---

## License

SwitchMTP is distributed under the **GNU General Public License v2.0** — see
[`LICENSE`](LICENSE).

```
SwitchMTP  Copyright (C) 2025 Francesco Fratei
Based on SwiftMTP by Neighbor_Z (https://github.com/Neighbor-Z/SwiftMTP)

This program is free software; you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation; either version 2 of the License, or
(at your option) any later version.
```
