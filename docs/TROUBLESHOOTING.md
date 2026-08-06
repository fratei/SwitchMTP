# Troubleshooting

---

## Device not detected

This is the most common issue on macOS. A frequent root cause is another process that has
already claimed the Switch's USB interface, making it unavailable to SwitchMTP.

### Why macOS claims MTP devices automatically

When a PTP/MTP device connects, macOS automatically attaches one or more system processes
to it before any user application can:

| Process | How to identify | What it does |
|---|---|---|
| **Image Capture Extension** / **PTPCamera** (`com.apple.ImageCaptureExtension2`) | `lsof` or Activity Monitor | Auto-claims USB class-6 (PTP/MTP) interfaces. Part of the Image Capture framework. |
| **Android File Transfer Agent** (`com.macroplant.AndroidFileTransfer.Agent` or similar) | Activity Monitor | Background daemon that watches for MTP devices and auto-launches Android File Transfer. |

When any of these holds the interface, SwitchMTP's call to `libusb_claim_interface`
fails. **Switch → Copy Diagnostics** and `switchmtp-cli doctor` report occupying
processes when the backend can identify them.

### How to fix it

**Quit Image Capture Extension and related processes:**

```sh
pkill -f "Image Capture Extension"
pkill -f "PTPCamera"
```

**Quit Android File Transfer Agent (if installed):**

```sh
pkill -f "Android File Transfer Agent"
```

Then unplug and replug the Switch, restart DBI's MTP responder, and try again.

You can also open **Activity Monitor**, search for the process names above, and force-quit
them from there.

> **Note:** Even after killing these processes, macOS may restart them automatically on
> the next USB event. A full reboot is the most reliable way to start with a clean USB
> stack. Community experience with DBI + macOS is that connection is intermittent — if one
> attempt fails, replug the cable, switch USB-C port sides or ports, and try again.

### Run the built-in doctor

```sh
# Inside the app bundle
/Applications/SwitchMTP.app/Contents/MacOS/switchmtp-cli doctor
```

`doctor` reports the current USB device list, any occupying PIDs, and actionable
guidance.

---

## Switch appears in a homebrew USB mode

**Symptom:** SwitchMTP reports that the Switch is in a non-MTP homebrew USB mode. The USB
device is Nintendo `0x057E:0x3000`.

**Cause:** PID `0x3000` is libnx's generic homebrew `usbComms` product ID. It can be
DBIbackend, Awoo-Installer's **Install from USB** mode, GoldLeaf USB mode, or another
homebrew vendor-specific USB protocol. None of these is MTP, and USB identity alone does
not identify which homebrew owns the interface.

SwitchMTP deliberately does not implement DBIbackend, Awoo/Tinfoil TUL0/TUC0, or
GoldLeaf/Quark protocols. It talks MTP to DBI.

**Fix:** Exit whatever homebrew is running, open DBI, then press **X → "Run MTP
responder"**.

See also: [Why doesn't SwitchMTP support Awoo-Installer?](AWOO_USB.md)

---

## "Extra buffers exceeded. Media write speed is too low" on the Switch

**Cause:** DBI is running in applet mode (typically launched from the Album). HorizonOS
gives applets less memory, so transfer buffers can overflow during large installs.

**Fix:** Launch DBI in title-override mode:
1. Hold **R** while launching any installed game from the home screen.
2. The homebrew menu opens with full application resources.
3. Open DBI from there, then start the MTP responder.

See [`DBI_SETUP.md`](DBI_SETUP.md) for the full explanation.

---

## Transfers stall or time out

- Try a different USB-C cable. Cheap cables with thin data wires are a common cause.
- For large installs, make sure DBI is running in title mode, not applet mode.
- Watch the Switch screen during installs. DBI reports install progress and errors there;
  SwitchMTP can only report that the file was sent to the install target.
- If transferring many small files to a normal browsable storage, try fewer files at a
  time to isolate the failing item.

---

## Files over 4 GB: size shown as "—"

**Cause:** The MTP protocol's `ObjectInfo` structure uses a 32-bit size field. For files
≥ 4 GB, DBI reports `0xFFFFFFFF` in that field (the overflow sentinel). SwitchMTP
attempts to retrieve the real size via `GetObjectPropValue(OPC_ObjectSize)`, but DBI may
not implement that operation for all storage types.

**What SwitchMTP does:** Rather than display a wrong 4 GB, SwitchMTP shows `—` in the
size column and marks the entry with `sizeUnknown`. The file can still be downloaded; the
true size is determined from the received byte count.

**Note on SD Card writes > 4 GB:** DBI automatically splits files over 4 GB into an
archived folder on the SD card. This is transparent to SwitchMTP during upload — just
drop the file onto the SD Card storage and DBI handles the split server-side.

---

## Rename / delete / "New Folder" greyed out

**Cause:** SwitchMTP only enables actions included in the storage capabilities reported
by the backend. This is expected for some storages:

- **Installed games** — virtual dumps; rename/delete/mkdir are not available.
- **Album** — typically read-only.
- **Gamecard** — read-only dump.
- **NAND install / SD Card install** — write-only install triggers; browsing operations
  are not available.

If a normally writable storage (SD Card, Saves) shows these options greyed out, it may
be a connection issue — disconnect, reopen DBI's MTP responder, and reconnect.

---

## Install target does not open as a folder

**Cause:** This is expected. DBI's SD/NAND install storages are write-only drop targets,
not browsable folders. SwitchMTP marks them as install targets, so use drag-and-drop,
the upload control, or **Switch → Install to SD Card…** / **Install to NAND…** instead
of double-clicking them.

---

## Gatekeeper: "SwitchMTP is damaged and can't be opened" or "cannot be verified"

**Cause:** The app is ad-hoc signed, not notarized. macOS quarantines it.

**Fix:**

```sh
xattr -dr com.apple.quarantine /Applications/SwitchMTP.app
```

Or right-click `/Applications/SwitchMTP.app`, choose **Open**, then confirm **Open** when
macOS asks.

See [README.md](../README.md) for why the app is not notarized and how to build from
source as an alternative.

---

## Collecting diagnostics for a bug report

1. In the app, choose **Switch → Copy Diagnostics**. This copies a pretty-printed JSON
   report to the clipboard.
2. Or from the command line:

   ```sh
   /Applications/SwitchMTP.app/Contents/MacOS/switchmtp-cli --json doctor > diagnostics.json
   ```

3. Attach `diagnostics.json`, or paste the clipboard report, to your GitHub issue at
   https://github.com/fratei/SwitchMTP/issues.

The diagnostics include: USB device enumeration, any process holding the USB interface
that the backend can identify, and connected-device capabilities. No personal files or
save data are included.
