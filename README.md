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

## Reporting a problem

Use **Help ▸ Report an Issue…** in the app. It opens the right form with your version and
macOS version already filled in and offers to put a diagnostics report on the clipboard.

- [**Troubleshooting**](docs/TROUBLESHOOTING.md) — check here first; it covers the
  common failures.
- [**How to write a useful report**](docs/REPORTING_ISSUES.md) — what the forms ask for,
  why, and what the automated triage will do with your report.
- [**Contributing**](CONTRIBUTING.md) — building from source, running the tests, and the
  handful of behaviours that look like bugs but are deliberate.
- [**Security**](SECURITY.md) — report vulnerabilities privately, not in an issue.

Every issue is read by a rule-based triage pass ([`scripts/triage`](scripts/triage)) that
runs daily and on every issue event. It labels the report, tells you if something required
is missing, links the documented answer when there is one, and queues a fix attempt when
the report is complete and reproducible. There is no language model involved and no
telemetry — you can read exactly what it will do before you file.

---

## Credits

SwitchMTP exists because of other people's work. Everything below was used in some form —
either as code that ships in the app, or as documentation that made the protocol
understandable.

### Code that ships in SwitchMTP

| Project | Author | Licence | What it does here |
|---|---|---|---|
| [SwiftMTP](https://github.com/Neighbor-Z/SwiftMTP) | Neighbor_Z | GPL-2.0 | The macOS app SwitchMTP is derived from. The SwiftUI interface, file browser and transfer UI all descend from it. |
| [go-mtpfs](https://github.com/hanwen/go-mtpfs) | Han-Wen Nienhuys / Google | New BSD | The MTP protocol engine — containers, transactions, object handling. Vendored and modified. |
| [ganeshrvel/go-mtpfs](https://github.com/ganeshrvel/go-mtpfs) | Ganesh Rathinavel | New BSD | The fork actually vendored, for its UTF-16 surrogate-pair handling and `SendObject` speed fixes. |
| [ganeshrvel/usb](https://github.com/ganeshrvel/usb) | Ganesh Rathinavel, from [hanwen/usb](https://github.com/hanwen/usb) | New BSD | Go bindings to libusb. |
| [libusb](https://libusb.info) | The libusb project | LGPL-2.1-or-later | All USB access. Built from source and bundled as a shared library, so it can be replaced — see [`THIRD_PARTY.md`](THIRD_PARTY.md). |

### What SwitchMTP talks to

- **[DBI](https://github.com/rashevskyv/dbi)** by duckbill / rashevskyv — the MTP responder
  that runs on the Switch. SwitchMTP is an unaffiliated client and bundles no part of DBI;
  you install it yourself.

### Documentation and research sources

No code from these is included, but the protocol work would have been guesswork without
them:

- **[Awoo-Installer](https://github.com/Huntereb/Awoo-Installer)** by Huntereb — the clearest
  description of the TUL0/TUC0 USB install protocol, written up in
  [`docs/AWOO_USB.md`](docs/AWOO_USB.md). The USB install code it inherits is originally by
  **Adubbz** (Tinfoil).
- **[ns-usbloader](https://github.com/developersu/ns-usbloader)** by developersu — reference
  for the same protocol. **Not a dependency**: SwitchMTP needs no Java and no external
  loader, and its GPL-3.0 licence would in any case be incompatible with this project's
  GPL-2.0. The reasoning is documented in [`docs/AWOO_USB.md`](docs/AWOO_USB.md).
- **[libnx](https://github.com/switchbrew/libnx)** and
  **[GoldLeaf](https://github.com/XorTroll/Goldleaf)** — established that USB product ID
  `0x3000` is libnx's *generic* homebrew `usbComms` ID, shared by several homebrew apps
  rather than unique to any one of them. That corrected a misidentification in this app.
- **[switchbrew](https://switchbrew.org)** — Horizon OS and USB documentation.
- The **MTP** (Media Transfer Protocol) and **PTP** (ISO 15740) specifications — the
  operation codes, property codes and container format the whole backend is built on, and
  the basis for handling files larger than 4 GB, where MTP's 32-bit size field overflows.
- **Android's MTP extensions**, which DBI advertises by default. SwitchMTP detects them for
  diagnostics but deliberately never calls them; see `backend/nxmtp/caps.go`.

See [`THIRD_PARTY.md`](THIRD_PARTY.md) for full attribution and license texts.

### Considered but not used

- **[go-mtpx](https://github.com/ganeshrvel/go-mtpx)** — a high-level MTP library that was an
  obvious fit, and the original SwiftMTP backend was built on it. It is **not used here**
  because it carries no licence at all, which makes it unsafe to vendor or redistribute. Its
  role is filled by `backend/nxmtp/`, written from scratch against the MTP specification and
  the vendored `go-mtpfs` engine.
- **ns-usbloader** — see above. Not needed, and licence-incompatible.

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
