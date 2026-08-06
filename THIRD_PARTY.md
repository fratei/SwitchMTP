# Third-Party Notices

SwitchMTP is a derivative work of **SwiftMTP** (GPL-2.0) and incorporates vendored
BSD-licensed libraries. This file satisfies GPL-2.0 §2(a) (prominent change notices)
and the attribution requirements of the New BSD (3-clause) licenses below.

---

## 1. SwiftMTP — GPL-2.0

> Copyright notice retained from the original work:
>
> Copyright (C) Neighbor_Z and contributors  
> Distributed under the GNU General Public License v2.0

- **Original repository:** https://github.com/Neighbor-Z/SwiftMTP  
- **License:** GNU General Public License v2.0 — see `LICENSE` at the root of this repo.

### Modifications made in SwitchMTP (GPL-2.0 §2(a) change notice)

SwitchMTP is based on SwiftMTP but is **substantially modified**:

| Area | Change |
|------|--------|
| Name / branding | Rebranded from SwiftMTP to SwitchMTP; bundle ID changed to `me.fratei.switchmtp`; all `Gomtp*` Swift types renamed to `Nxmtp*`. |
| AI features | `AIManager.swift` and `AINLSearch.swift` removed entirely, along with their Settings/MainView UI, strings, and all network requests. |
| Go backend | The original `lib/gomtp.dylib` (closed source, part of SwiftMTP) and the `ffi/kalam/native/` single-device FFI source are **replaced in their entirety** by a new Go backend (`backend/`) written from scratch on `go-mtpfs/mtp` directly. The FFI contract (§4 of the implementation plan) is preserved at the envelope level for Swift-side compatibility but all Go source is new. |
| Third-party dependencies | `go-mtpx` (upstream: `ganeshrvel/go-mtpx`) is **not used** — see §5 below. |
| Switch/DBI features | Switch-specific storage classification, DBIbackend mode detection, install-storage drop targets, applet-vs-title mode guidance, and the `FetchDiagnostics` export are all new. |
| App icon | The original MTPIcon is replaced with a new icon. |
| Screenshots / materials | Upstream `Materials/` folder, `Comparison.md`, and `Details_and_Privacy.md` are not included. |
| Update checker | Repointed at `github.com/fratei/SwitchMTP` releases. |

Because SwitchMTP is a derivative work distributed under the same GPL-2.0 terms, the
complete corresponding source code is available at https://github.com/fratei/SwitchMTP.

---

## 2. go-mtpfs — New BSD (3-clause)

Vendored at `third_party/go-mtpfs/`.

```
New BSD License

Copyright (c) 2012 Google Inc. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Ivan Krasin nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

- **Original repository:** https://github.com/hanwen/go-mtpfs (author: Han-Wen Nienhuys)  
- **Fork used:** https://github.com/ganeshrvel/go-mtpfs (fork by Ganesh Rathinavel)

**Note on GitHub license detection:** GitHub's license detector reports `NOASSERTION` for
this repository solely because the LICENSE file's lines are prefixed with `//` (Go-comment
style). The text is verbatim New BSD (3-clause). This has been verified by reading
`third_party/go-mtpfs/LICENSE` directly.

**Modified vendored files:**

| File | Change notice |
|------|---------------|
| `third_party/go-mtpfs/mtp/usb_diag_darwin.go` | Adds macOS USB occupier-PID diagnostics and an `isOwnProcess()` exclusion for SwitchMTP command names (`SwitchMTP`, `switchmtp-cli`, `switchmtp-doctor`) while retaining the upstream-compatible `SwiftMTP` exclusion. |

The fork already incorporates the other fixes we depend on (UTF-16 surrogate-pair
handling and `SendObject` speed tuning).

---

## 3. ganeshrvel/usb (derived from hanwen/usb) — New BSD (3-clause)

Vendored at `third_party/usb/`.

```
New BSD License

Copyright (c) 2012 Google Inc. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Ivan Krasin nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

- **Original repository:** https://github.com/hanwen/usb  
- **Fork used:** https://github.com/ganeshrvel/usb (fork by Ganesh Rathinavel)

**Note on GitHub license detection:** Same `//`-prefixed file situation as go-mtpfs above.

**Modified vendored files:**

| File | Change notice |
|------|---------------|
| `third_party/usb/usb.go` | Replaces upstream `pkg-config` libusb discovery with explicit cgo include/library paths and a development-only rpath for the repo-local universal libusb build. |

Upstream declared:
```go
// #cgo pkg-config: libusb-1.0
```
We replaced this with explicit cgo directives:
```go
// #cgo darwin CFLAGS:  -I${SRCDIR}/../libusb/include/libusb-1.0
// #cgo darwin LDFLAGS: -L${SRCDIR}/../libusb/lib -lusb-1.0
// #cgo darwin LDFLAGS: -Wl,-rpath,${SRCDIR}/../libusb/lib
```
This causes the Go backend to link against the universal `libusb.dylib` built by
`scripts/build-libusb.sh` rather than a Homebrew-provided library, making the build
hermetic (no pkg-config, no Homebrew required). The development-only cgo rpath is removed
from shipped binaries by `scripts/build-backend.sh`.

---

## 4. libusb — LGPL-2.1-or-later

- **Homepage:** https://libusb.info  
- **License:** GNU Lesser General Public License v2.1 or later  
- **Version bundled:** 1.0.27 (pinned in `scripts/build-libusb.sh`)  
- **How bundled:** Built from source by `scripts/build-libusb.sh` as a universal macOS
  dylib and shipped as a **separate dynamic library** (`Contents/Frameworks/libusb-1.0.0.dylib`)
  inside the app bundle. It is **not** statically linked.

**LGPL compliance:** The LGPL-2.1 permits use in non-LGPL applications provided the
covered library is linked dynamically and users can replace it with a modified version.
SwitchMTP satisfies both requirements:

1. **Dynamic linking.** `nxmtp.dylib` (the Go backend) links against
   `@loader_path/libusb-1.0.0.dylib` at runtime. There is no static incorporation of
   libusb object code into any SwitchMTP binary.
2. **User replaceability (LGPL §6).** Because `libusb-1.0.0.dylib` is a discrete file in
   `Contents/Frameworks/`, a user may replace it with a modified build of libusb 1.0.27
   by substituting that file. No re-linking of SwitchMTP's own code is required.

The libusb source code is available at https://github.com/libusb/libusb.

---

## 5. go-mtpx — NOT USED

`ganeshrvel/go-mtpx` (and its upstream `ganeshrvel/go-mtpx`, originally derived from
`OpenMTP`) carries **no license file** anywhere in the repository tree. Because it cannot
be lawfully vendored without a license grant, SwitchMTP does **not** use, copy, or derive
from go-mtpx. The `backend/nxmtp/` package is a clean-room replacement written directly
on `go-mtpfs/mtp`.

---

## 6. DBI — independent third-party application (not bundled)

**DBI** is a Nintendo Switch homebrew application by duckbill and rashevskyv, distributed
at https://github.com/rashevskyv/dbi. SwitchMTP is an **unaffiliated MTP client** that
speaks to DBI's standard MTP responder the same way any MTP host would. DBI is not
bundled with SwitchMTP, no DBI code or assets are incorporated, and SwitchMTP's authors
have no relationship with the DBI project.

---

## Adding GPL-2.0 Copyright Headers to SwitchMTP Source Files

Per GPL-2.0 §1, each modified Swift/Go source file in this repository that derives from
SwiftMTP should carry a header of the form:

```
// SwitchMTP — <short description>
// Copyright (C) <year> <author>
// Based on SwiftMTP by Neighbor_Z (https://github.com/Neighbor-Z/SwiftMTP)
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <https://www.gnu.org/licenses/>.
```

New files not derived from SwiftMTP (e.g. the entire `backend/` tree) may carry the
same header without the "Based on SwiftMTP" line.
