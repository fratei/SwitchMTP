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
