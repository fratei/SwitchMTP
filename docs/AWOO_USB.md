# Awoo-Installer / Tinfoil USB support — evaluation and deferral

**Status:** deferred to a future release. SwitchMTP v1 detects these devices and explains the
situation, but does not implement their protocol.

**Question this answers:** *"Do I need ns-usbloader?"* — **No.** Neither for SwitchMTP, nor for
anything SwitchMTP plans to do. See [Why ns-usbloader is not required](#why-ns-usbloader-is-not-required).

---

## 1. What this is about

SwitchMTP talks **MTP** to **DBI's MTP responder**. There is a second, unrelated ecosystem of
Switch USB installers — **Awoo-Installer**, **Tinfoil**, **GoldLeaf** — that do *not* speak MTP.
They speak their own protocols over a vendor-specific USB interface.

This document records what we learned about that ecosystem, why v1 does not support it, and
exactly what a future implementation would need to do.

## 2. The `0x057E:0x3000` collision (important)

`0x3000` is **libnx's generic homebrew `usbComms` product ID**. It is not owned by any one
application. Every homebrew that opens a vendor-specific USB interface shows up as
`057E:3000`:

| Homebrew | Mode | Protocol |
| --- | --- | --- |
| DBI | "Install title from DBIbackend" | DBI's own binary protocol |
| Awoo-Installer | "Install from USB" | Tinfoil `TUL0`/`TUC0` |
| GoldLeaf | USB mode | Quark |

**Consequence:** from USB identity alone we *cannot* tell which one is running. SwitchMTP
therefore reports the neutral profile `homebrewUSB` and gives advice that is correct in all
three cases ("exit it, open DBI, press X, Run MTP responder"). An earlier version of this
project incorrectly claimed the device was in DBIbackend mode specifically; that was wrong and
has been fixed.

### How it is distinguished from DBI's MTP responder

The two are unambiguously separable at the USB descriptor level, which is why there is no risk
of confusing them:

| | DBI MTP responder | Awoo / Tinfoil / GoldLeaf |
| --- | --- | --- |
| Product ID | `0x201D` (shared with Horizon OS) | `0x3000` |
| Interface class | PTP / Still Image | Vendor-specific (`0xFF`) |
| Endpoints | **3** (bulk-in, bulk-out, interrupt-in) | **2** (bulk-in `0x81`, bulk-out `0x01`) |

Our discovery layer matches interfaces with *exactly three* endpoints, so an Awoo device can
never be mistaken for an MTP device — but it also means that without explicit detection we would
show the user nothing at all. Hence the `homebrewUSB` profile.

## 3. The protocol, if we implement it later

Pull-based and little-endian throughout. The **Switch drives**; the host is a file server.

### Handshake

Host → Switch:

```
"TUL0"                    4 bytes, magic
u32                       byte length of the name list
8 bytes                   padding (zero)
<names>                   filenames joined by "\n"
```

### Command loop

Switch → host, 32-byte header:

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 4 | `"TUC0"` magic |
| 4 | 1 | type — `0` = REQUEST, `1` = RESPONSE |
| 5 | 3 | padding |
| 8 | 4 | command id |
| 12 | 8 | data size |
| 20 | 12 | padding |

Command ids: `0` = EXIT, `1` = FILE_RANGE, `2` = FILE_RANGE_ALT. **Treat 1 and 2 identically** —
both request a byte range; the distinction is a historical artefact.

`FILE_RANGE` payload (Switch → host):

```
u64 range_size
u64 range_offset
u64 name_len
u64 padding
<filename>                name_len bytes
```

Host reply — the 12-byte standard reply, then the raw bytes:

```
"TUC0", type=1, cmdId=1   12 bytes
u64 range_size
12 bytes                  padding
<data>                    streamed in 8 MiB (0x800000) chunks
```

### Implementation notes

- Bulk-out needs **zero-length-packet** handling when a write lands exactly on the packet
  boundary.
- The **first** command can take a long time (the Switch may be showing a UI); allow ≥10 s
  before treating a read timeout as fatal.
- Split archives (`game.nsp/00`, `/01`, …) must be presented as one logical file: the reader has
  to map a global offset onto the right chunk file. ~50 lines.
- Total estimate: roughly 150–200 lines of Go for the protocol, plus the split reader.

## 4. Why it is deferred

- **No speed benefit.** Both paths are bounded by the Switch's eMMC/SD write speed, not by the
  host protocol. *(Not benchmarked — asserted from the architecture.)*
- **One genuine robustness advantage:** because the Switch pulls, it cannot be overrun by the
  host. This structurally avoids the *"Extra buffers exceeded. Media write speed is too low"*
  failure DBI users hit in applet mode. That is a design-level observation; we have not measured
  how much it matters in practice.
- **It is install-only.** There is no browse, no download, no save management, no delete. It
  could never replace the MTP path — only sit beside it as a second install transport.
- **The upstream project is dormant** (no substantive activity since ~2021–22).
- **Scope discipline.** v1's job is to make DBI/MTP genuinely good on macOS.

The net value is *reach* — users who already run Awoo and not DBI — rather than capability.
That is a reasonable v2 feature, not a v1 blocker.

## 5. Licensing constraints on a future implementation

Read this before writing any code.

- **Awoo-Installer** — the repository LICENSE is **GPL-3.0**, *but* the three files that define
  the protocol (`usb_util.hpp`, `usb_util.cpp`, `usbInstall.cpp`) carry **MIT** headers
  inherited from Adubbz's original Tinfoil code. Implementing from those MIT-headered files is
  safe.
- **ns-usbloader** — **GPL-3.0**. Use for cross-referencing behaviour only; **never translate
  its code**. GPL-3.0 is in any case incompatible with this project's GPL-2.0 licensing.
- Anything written must be **clean-room from the protocol description**, and attributed in
  `THIRD_PARTY.md`.

## 6. Why ns-usbloader is not required

`ns-usbloader` is a **host-side Java application** that implements the same `TUL0`/`TUC0`
protocol described above. It is one client of an open protocol, not a component, driver or
dependency.

SwitchMTP does not use it, does not need it, and requires no Java runtime of any kind:

- For **browsing, downloading, uploading, saves, album and gamecard**, SwitchMTP speaks MTP
  directly to DBI. ns-usbloader cannot do any of these things.
- For **installing**, SwitchMTP uses DBI's own MTP install storages.
- If the Tinfoil transport is ever added, it will be implemented natively in Go inside
  `nxmtp`, from the protocol specification in §3 — still with no Java dependency.

## 7. Unverified claims

Recorded honestly so nobody treats them as established:

- Relative transfer speed of MTP/DBI vs Tinfoil/Awoo — never benchmarked.
- DBI's exact VID:PID for the MTP responder. `057E:201D` is community-established and matches
  libmtp's device table, but DBI is closed-source and this is not vendor-confirmed.
- The precise condition under which the Switch sends `FILE_RANGE_ALT` (cmdId 2) instead of
  `FILE_RANGE` (cmdId 1). Handling them identically is what every known implementation does.
