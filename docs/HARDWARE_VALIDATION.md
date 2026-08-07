# Hardware validation protocol

Use this protocol only with your own Nintendo Switch and your own files. The default harness run is read-only and should be the first pass on any console.

## Preconditions

- A USB-C **data** cable. Charge-only cables will not expose MTP.
- A built app/CLI: `scripts/build-app.sh` must produce `build/Release/SwitchMTP.app/Contents/MacOS/switchmtp-cli`.
- A Switch running DBI with MTP support. In DBI, press **X → Run MTP responder**.
- Record how DBI was launched:
  - **Title mode**: hold **R** while starting a game, then launch DBI. Required for large installs/transfers.
  - **Applet mode**: launching DBI from Album without holding R. Large transfers can fail with `Extra buffers exceeded` / `media write speed too low`; that is a DBI mode limitation, not automatically a SwitchMTP bug.

## Run order

1. Read-only discovery:

   ```sh
   scripts/hw-validate.sh --mode title
   ```

2. Opt-in write probes. These create `/switchmtp-hwtest-<timestamp>/` only on the SD Card storage, upload tiny files, then remove the scratch directory. The harness never writes to NAND or to unknown custom mappings:

   ```sh
   scripts/hw-validate.sh --mode title --write-tests
   ```

3. Opt-in large-file read. Choose a file you own that is larger than 4 GiB, preferably on SD, and run in title mode:

   ```sh
   scripts/hw-validate.sh --mode title --large-file '65537:/path/to/large-file.nsp'
   ```

Every run writes one report: `build/hw-report-<timestamp>.md`.

## Probe guide

| Probe | What it proves | If it FAILs |
|---:|---|---|
| 1 | Whether listings use `GetObjectPropList` or safely fall back to per-object `GetObjectInfo`. | A listing failure is a real backend/DBI interop bug. Attach the report. A fallback is expected-and-handled. |
| 2 | Whether `GetObjectPropValue(ObjectSize)` is advertised and sampled listings avoid bogus 4 GiB sizes. | Unknown sizes with advertised support may be a backend bug; otherwise unsupported ObjectSize is expected-and-handled by showing `—`. Use probe 5 for proof. |
| 3 | Whether `SetObjectPropValue(ObjectFileName)` rename works in a scratch directory. | Unsupported rename is expected-and-handled if the app disables it. Advertised-but-failing rename is a bug to file. |
| 4 | Whether `MoveObject` / `CopyObject` capabilities derive a safe `canMove` value. | Inconsistent capability derivation is a real bug. The current CLI does not expose a device-to-device move/copy runtime command. |
| 5 | Whether a >4 GiB file can be read end to end and the local byte count matches ObjectSize. | In applet mode, retry in title mode first. In title mode, a mismatch or failed copy is a real transfer bug. |
| 6 | Whether all nine fixed DBI storages classify correctly: SD, NAND User/System, Installed Games, SD/NAND Install, Saves, Album, Gamecard. Custom storages are optional. | Missing or wrong kinds are app/backend classification bugs unless DBI was not in MTP responder mode. |
| 7 | Whether SD/NAND install targets are `installTarget=true` and `browse=false`. | A browsable install target is a UI/backend bug; the app must not list write-only DBI install triggers. |
| 8 | Whether non-ASCII / emoji filenames round-trip through upload/listing. | A mismatch is a filename encoding bug. Keep the report and note the exact filename. |
| 9 | Whether repeated operations survive the session/reconnect pattern instead of requiring manual replugging. | Repeated failures indicate a reconnect/session-lifetime bug or DBI instability; retry after replug, then file with the report. |

## Bug report attachments

Attach:

- The full `build/hw-report-<timestamp>.md` file.
- A fresh JSON doctor capture if requested:

  ```sh
  build/Release/SwitchMTP.app/Contents/MacOS/switchmtp-cli --json doctor > build/doctor.json
  ```

Also state DBI version, applet vs title mode, Switch model/firmware if known, and whether the USB cable was known-good for data.

---

## Verified results — Nintendo Switch + DBI

Captured against a real console: **HOS 22.5.0**, DBI MTP responder, macOS 26.6 (arm64).

### Device identity

| Property | Value |
|---|---|
| USB VID:PID | `0x057E:0x201D` |
| USB product string | `DBI` |
| USB interface string | `DBI MTP` |
| USB interface class | 6 (still image / PTP), 3 endpoints |
| MTP Manufacturer / Model | `Nintendo` / `Switch` |
| MTP DeviceVersion | `22.5.0` |
| MTP extensions | `microsoft.com: 1.0; android.com: 1.0;` |

> **DBI is only identifiable from the USB descriptors.** Its MTP `DeviceInfo` reports
> `Nintendo`/`Switch`, which is indistinguishable from stock Horizon OS. SwitchMTP
> classifies the profile from the USB product and interface strings for this reason.

### Operations actually supported

`0x1001-0x1005, 0x1007-0x1009, 0x100b-0x100d, 0x1014-0x1016, 0x1019, 0x101b,
0x95c1-0x95c5, 0x9801-0x9805, 0x9808`

Resolving the open questions from the implementation plan:

| Operation | Code | Supported | Consequence |
|---|---|---|---|
| `GetObjectPropList` | `0x9805` | **Advertised, but rejects our parameters** (`ParameterNotSupported`) | Listing degrades to `GetObjectHandles` + `GetObjectInfo` automatically |
| `GetObjectPropValue` | `0x9803` | Yes | True size available for files >4 GB |
| `SetObjectPropValue` | `0x9804` | Yes | Rename works |
| `GetPartialObject` | `0x101b` | Yes | Resumable / ranged reads |
| Android `GetPartialObject64` | `0x95c1` | Yes | >4 GB reads |
| `MoveObject` | `0x1019` | Yes | Native move |
| `CopyObject` | `0x101a` | **No** | Copy falls back to download + upload |
| `GetNumObjects` | `0x1006` | **No** | Never called |
| `GetThumb` | `0x100a` | **No** | No thumbnails |
| `ResetDevice` | `0x1010` | **No** | Recovery uses USB-level reset instead |

### Storages enumerated

All nine, correctly classified: `SD Card` (65537), `Internal (User)` (65538),
`Internal (System)` (65539), `Install to SD Card` (65541), `Install to Internal` (65542),
`Saves` (65543), `Album` (65544), and the user-defined `DBILogs` (65792).

### Operations exercised

| Test | Result |
|---|---|
| Detect + identify | Pass — profile `switchDBI`, "Nintendo Switch (DBI)" |
| Enumerate storages | Pass — 9/9 with correct capability flags |
| Browse SD card root | Pass (via the `GetObjectInfo` fallback path) |
| Download 29 B file | Pass — content verified |
| Download 2.34 MB file | Pass — 25 MB/s |
| Upload 2.86 MB file | Pass — 2.8 MB/s, live progress |
| **Round-trip integrity** | **Pass — SHA-256 identical after upload then download** |
| `mkdir` | Pass |
| Rename | Pass |
| Delete file / delete directory | Pass |
| `doctor` with device connected | Pass — correctly names `ptpcamerad` |
| GUI app, sandboxed | Pass — claims the interface from `ptpcamerad` |

### The macOS interface conflict

`ptpcamerad` held the interface on **every** connection attempt. The recovery
(re-enumerate the port, then re-claim immediately) succeeded **5/5**.

Timing matters: an early implementation paused 120 ms after the reset before re-claiming
and lost the race *every* time, because `launchd` restarts `ptpcamerad` within roughly
that window. The claim must be issued with no delay.
