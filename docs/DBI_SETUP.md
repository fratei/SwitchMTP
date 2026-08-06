# DBI Setup Guide

This page walks you through getting DBI onto your Nintendo Switch and starting its MTP
responder so SwitchMTP can connect.

---

## Step 1 — Install DBI on your Switch

DBI requires a modded Switch (CFW such as Atmosphère). Get the latest release from the
official repository:

> **https://github.com/rashevskyv/dbi/releases**

Download `dbi.nro` and copy it to `sdmc:/switch/dbi.nro` on your Switch's SD card.
Do not download DBI from unofficial mirrors.

---

## Step 2 — Launch DBI and start the MTP responder

1. On your Switch home screen, open DBI from the homebrew menu.
2. Open **DBI**.
3. In DBI's main menu, press **X** to open the tools menu.
4. Select **"Run MTP responder"**.

The Switch screen will show a waiting indicator. The device is now in MTP mode.

For large installs, launch the homebrew menu in **title mode** instead of from the
Album applet; see the critical note below.

---

## Step 3 — Connect to your Mac

Connect the Switch to your Mac using a **USB-C data cable**. Charge-only cables do not
carry data — if SwitchMTP does not detect the device, try a different cable.

SwitchMTP should detect the Switch within a few seconds and populate the sidebar with
the available storages.

---

## Critical: Applet mode vs. title mode

> **This is the most common cause of failed installs on large files.**

When DBI runs launched from the Album (applet mode), HorizonOS gives it a reduced memory
and CPU allocation. For large NSP / NSZ / XCI / XCZ installs, the MTP transfer buffers fill
faster than the SD card can write, and DBI shows:

> **Extra buffers exceeded. Media write speed is too low**

**Fix:** Launch DBI in title-override mode instead:
1. Make sure DBI is installed as a forwarder title, *or* use any installed game as the
   launch vehicle.
2. Hold **R** while launching an installed game from the home screen. This opens the
   homebrew menu with full application-level resources.
3. Select DBI from there, then start the MTP responder as above.

This is also beneficial for long file manager operations on a slow SD card.

---

## Critical: MTP mode vs. homebrew USB mode (common mistake)

The Switch homebrew ecosystem reuses Nintendo USB PID `0x057E:0x3000` for vendor-specific
homebrew USB protocols. None of these modes is MTP:

| Homebrew / menu option | USB PID | Protocol |
|---|---|---|
| **X → "Run MTP responder"** ← *use this* | `0x201D` | Standard MTP — SwitchMTP works here |
| DBI → "Install title from DBIbackend" | `0x3000` | DBI's own binary protocol — **not MTP** |
| Awoo-Installer → "Install from USB" | `0x3000` | Tinfoil TUL0/TUC0 — **not MTP** |
| GoldLeaf USB mode | `0x3000` | Quark — **not MTP** |

If you select one of those modes, the Switch presents USB PID `0x3000`. USB identity
alone does not say which homebrew owns it, but it does tell SwitchMTP that the device is
not speaking MTP.

SwitchMTP detects this non-MTP homebrew USB mode and tells you to switch modes:

> Exit the current homebrew, open DBI, then press X on DBI's main menu → "Run MTP responder".

The remedy is the same whether you came from DBIbackend, Awoo-Installer, or GoldLeaf:
exit whatever homebrew is running, open DBI, and select the MTP responder.

---

## DBI storages exposed over MTP

Once connected, SwitchMTP classifies each DBI storage by what it actually is and by the
capabilities the device exposes. The sidebar groups storages as **Storage**,
**Install**, **Dumps**, and **System**.

| Kind shown by SwitchMTP | Typical capability | Notes |
|---|---|---|
| **SD Card** | Browse / read / write, plus delete/rename/mkdir when DBI advertises them | General file management on the microSD card. |
| **Saves** | Browse / read; write only when DBI exposes writable save access | Used by **Back Up Saves…** and **Restore Saves…**. Writing here overwrites console save data. |
| **Album** | Browse / read; delete only when DBI advertises it | Screenshots and videos. |
| **Installed Games** | Browse / read | Virtual/generated storage: DBI synthesizes dump files on demand. It is not a normal writable folder. |
| **Game Card** | Browse / read | Virtual/generated storage for the inserted game card. Dumping can be slow; leave the card inserted until the transfer ends. |
| **Install to SD Card** | Write-only install target | Drop `.nsp`, `.nsz`, `.xci`, or `.xcz` files here to send them to DBI's SD install target. |
| **Install to Internal** | Write-only install target | Same, but targets NAND/internal storage. |
| **Internal (User)** / **Internal (System)** | Browse / read | System storages. SwitchMTP treats them conservatively. |
| Custom/unknown storages | Based on the storage's advertised MTP access flags | User-defined DBI storages fall back to normal MTP capability detection. |

> **Write-only install storages:** SD Card install and NAND install are not folders you
> browse — they are drop targets. Double-clicking them does not open a folder because the
> app deliberately marks them as not browsable.

---

## Switch menu workflows

SwitchMTP's menu bar includes a top-level **Switch** menu:

- **Back Up Saves…** asks for a parent folder, creates a timestamped `Switch Saves
  yyyy-MM-dd HH-mm-ss` folder inside it, then downloads the whole Saves storage.
- **Restore Saves…** asks for a backup folder, then shows a critical confirmation.
  This is destructive and irreversible: files from the selected folder replace save data
  on the console. It is enabled only when the Saves storage is present and writable, so
  DBI must have save write access enabled.
- **Export Album…** creates a timestamped `Switch Album ...` folder under the folder you
  choose and downloads the Album storage.
- **Dump Gamecard…** creates a timestamped `Switch Gamecard ...` folder and reads DBI's
  virtual Game Card storage. This can take a long time; keep the game card inserted.
- **Install to SD Card…** / **Install to NAND…** appear when DBI exposes install
  targets. The file picker accepts only `.nsp`, `.nsz`, `.xci`, and `.xcz`.
- **Copy Diagnostics** stays enabled even when disconnected and copies a JSON report to
  the clipboard for bug reports.

---

## Installing an NSP / NSZ / XCI / XCZ

1. In SwitchMTP, select **"Install to SD Card"** or **"Install to Internal"** in the
   sidebar, or use **Switch → Install to SD Card…** / **Install to NAND…**.
2. Drag your file(s) onto the storage, or use the upload button.
3. SwitchMTP transfers the file. DBI starts installing when the file lands in the install
   target and shows progress on the Switch screen.
4. There is **no MTP-level completion event** for installation. SwitchMTP can report only
   that the file was sent; it cannot report "installed" or verify final install success.

---

## Unknown sizes shown as `—`

Some DBI virtual files are larger than MTP's 32-bit `ObjectInfo` size field. When that
field overflows, DBI may report `0xFFFFFFFF`, and it does not always provide a reliable
64-bit `ObjectSize` property. SwitchMTP shows `—` instead of pretending the file is a
wrong 4 GiB size.

---

## Cable quality

Use a USB-C **data** cable. Charge-only cables do not carry USB data, so the Switch will
not appear to SwitchMTP.
