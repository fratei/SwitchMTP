# Troubleshooting

---

## Device not detected

On macOS the usual cause is another process holding the Switch's USB interface.
**SwitchMTP recovers from the common case automatically** — the notes below explain
what it is doing and what to try when the automatic recovery is not enough.

### Why macOS claims the Switch automatically

DBI's MTP responder presents a USB **still-image class** interface (class 6), the same
class digital cameras use. macOS binds every such interface to its own PTP daemon,
`/usr/libexec/ptpcamerad`, the moment it enumerates — before any application can ask for
it. The daemon holds the interface *exclusively*, so `libusb_claim_interface` fails with
`LIBUSB_ERROR_ACCESS`.

This is not a misconfiguration and it is not something you did wrong: on a stock Mac,
`ptpcamerad` is holding your Switch essentially every time you plug it in.

| Process | What it does |
|---|---|
| **`ptpcamerad`** | macOS's PTP/camera daemon. Auto-claims class-6 interfaces. `launchd` restarts it on demand, so killing it does not keep it away. **Handled automatically.** |
| **Image Capture Extension** / **PTPCamera** | Older equivalents of the above on earlier macOS versions. |
| **Android File Transfer Agent** | Third-party daemon that watches for MTP devices. Quit it and remove it from Login Items. |
| **Other MTP clients** (OpenMTP, gphoto2, another copy of SwitchMTP) | Only one program can hold an MTP device at a time. |

### How SwitchMTP handles it

When a claim fails with `LIBUSB_ERROR_ACCESS`, SwitchMTP re-enumerates the USB port.
That makes `ptpcamerad` release the interface, and SwitchMTP claims it in the same
instant — before `launchd` can restart the daemon and let it grab the interface again.
It retries a few times if it loses the race.

Two things worth knowing:

- **It needs no privileges.** SwitchMTP never kills `ptpcamerad`, never asks for an
  administrator password, and works from inside the App Sandbox.
- **`ptpcamerad` will still be listed** as a process on the device even when everything
  is working, because it re-attaches at the device level. `switchmtp-cli doctor` only
  reports processes holding the *interface*, which is what actually blocks a claim.

SwitchMTP explains this in the app the first time you launch it, before it touches the USB
bus at all. You can read that notice again at any time from **Help → How SwitchMTP
Connects to Your Switch**.

### If it still fails

1. **Quit other MTP software** — Android File Transfer, OpenMTP, and any other copy of
   SwitchMTP (including one left running in the background).
2. **Quit Image Capture and Photos**, and dismiss any "import photos?" prompt.
3. **Unplug and reconnect the Switch**, then restart DBI's MTP responder
   (main menu → **X** → *Run MTP responder*).
4. **Run the diagnostics**: `switchmtp-cli doctor`, or **Switch → Copy Diagnostics** in
   the app. It names the process holding the interface and what to do about it.
5. **Try a different USB-C cable.** Many cables carry power only. This is the second most
   common cause of "not detected" after interface conflicts.


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

SwitchMTP shows a **"No progress reported for N seconds"** notice when the console has gone
quiet, and the Cancel button in the transfer bar is always available. Cancelling asks the
backend to stop, but a USB transfer that is already blocked inside the kernel cannot be
interrupted; it has to time out first, which can take up to two minutes. If the app still
appears busy after that, unplug the cable — SwitchMTP detects the disconnection and
recovers.

---

## The bar reaches 100% and then sits at "Installing on the console…"

**This is expected, and it is not a stall.** DBI writes the title to storage only after the
final byte has arrived, and that step produces no MTP traffic at all — there is no protocol
event for "installation finished". SwitchMTP therefore cannot show a percentage for it. The
elapsed counter keeps running so you can see the app is still connected and waiting.

The real progress is on the Switch screen. Large titles can take several minutes here.

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

## Exported files are not in ~/Downloads

**Cause:** When **iCloud Drive → Desktop & Documents Folders** is enabled, the
**Downloads** shortcut in the macOS save panel's sidebar points at *iCloud Drive's*
Downloads folder, not your home `~/Downloads`. This is macOS behaviour, not something
SwitchMTP controls — the app writes exactly where the panel told it to.

**Fix:** Look in

```sh
~/Library/Mobile\ Documents/com~apple~CloudDocs/Downloads
```

To avoid the ambiguity, pick the destination explicitly in the export panel (press
**⇧⌘G** inside the panel and type an absolute path) rather than using the sidebar
shortcut. The transfer summary in the app also shows the resolved destination path.

> Note: **⇧⌘G** in the *main window* is SwitchMTP's own **Go to Folder**, which navigates
> to an absolute path **on the Switch**. Inside a macOS open/save panel the same shortcut
> is the system's Go to Folder for local paths.

---

## Dates all show "—"

**Cause:** DBI's MTP responder does not report creation or modification dates for any
object — every `ObjectInfo` comes back with empty date fields. SwitchMTP shows `—` rather
than inventing a timestamp, because filling in "now" would make an entire SD card look
like it had just been rewritten.

This is a limitation of the responder, not a bug. Sorting by **Date Modified** still works;
undated entries sort last.

---

## A USB device that is not a Switch appears in the sidebar

**Cause:** MTP devices are found by their USB endpoint layout (one bulk-in, one bulk-out,
one interrupt-in). Some unrelated devices — USB Ethernet adapters especially — use the
same layout.

SwitchMTP filters these out by USB interface class: it accepts still-image (class 6, which
is what DBI reports) and vendor-specific interfaces that name themselves "MTP". If a
device still slips through, include the output of `switchmtp-cli --json doctor` in a bug
report.

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

### Verbose transfer log

For problems that only reproduce in the GUI, enable the app's file log:

```sh
defaults write me.fratei.switchmtp debugLogEnabled -bool YES
```

Relaunch the app, reproduce the problem, then read:

```sh
cat ~/Library/Containers/me.fratei.switchmtp/Data/tmp/switchmtp-debug.log
```

Turn it off again with `defaults delete me.fratei.switchmtp debugLogEnabled`. The log
records transfer requests, resolved destination paths and backend result envelopes. It
contains file and folder names from your device, so review it before attaching it to a
public issue.

