# Linux packaging

This directory holds the Linux-side assets for SwitchMTP. There is no Linux GUI
and, on current evidence, there should not be one — see "How this fits alongside
your desktop" below. The Go backend and the `switchmtp` command line tool both
run on Linux today, and the CLI is the Linux product. What is here is the piece
that has to be right before it can work: USB access.

The quickest way to find out whether a machine is set up correctly is to build
the tool and ask it:

```sh
cd ../../backend && go build -o /tmp/switchmtp ./cmd/switchmtp && /tmp/switchmtp doctor
```

`doctor` checks for the rule below, notices when an equivalent one has already
been installed by another Switch tool, lists any process holding the USB device,
and prints what to do about whatever it finds.

## How this fits alongside your desktop

**Unlike on macOS, your desktop can already do most of this.** Verified against
Ubuntu 24.04 with a Switch running DBI: GNOME's `gvfs` auto-mounts the console,
lists all eight storages including the two install targets, and reads and writes
files with no setup at all. If you only want to copy files to the SD card, use
your file manager — you do not need this tool.

`switchmtp` is worth installing for the things `gvfs` does not do:

- **Installing games**, one after another, from a queue (`switchmtp install a.nsp b.nsp`).
  Copying several titles at once through a file manager asks DBI to do something
  it does not want to do.
- **Telling you what a storage is for.** `gvfs` shows DBI's install targets as
  ordinary folders; drop the wrong file in and you get `libmtp error: Could not
  send object`. `switchmtp` labels them drop-only and names the four extensions
  they accept.
- **Long transfers**, where reliability and progress reporting matter.

### ⚠️ `switchmtp` and your file manager cannot share the device

They do not fight over it in the way you might expect — `gvfsd-mtp` keeps the USB
node open the whole time and `switchmtp` still works, because on Linux contention
happens when a program claims the USB *interface*, not when it opens the device.

What actually breaks is `gvfs`'s cached session. **Once `switchmtp` has touched
the console, the existing mount goes stale**, and every subsequent write through
the file manager fails almost instantly with:

```
libmtp error:  Could not send object info.
```

This is not corruption and nothing is lost. Remount to recover:

```sh
gio mount -u mtp://<host>/ && gio mount mtp://<host>/
```

or unplug and replug the cable. Installing the udev rule below avoids the problem
altogether by stopping `gvfs` claiming the console in the first place — which is
the main reason to install it even on a machine where USB permissions already
work.

## Installing

### Debian, Ubuntu and derivatives

```sh
scripts/build-deb.sh
sudo apt install ./build/switchmtp_*.deb
```

This is the recommended route, for a reason worth stating: **a package can install
the udev rule, and a Flatpak or AppImage cannot.** That rule is the difference
between the tool working and the desktop claiming the console first, so any
format that cannot ship it leaves every user with a manual `sudo` step.

The package installs `switchmtp` to `/usr/bin`, the rule to
`/usr/lib/udev/rules.d/` — the vendor directory, leaving `/etc/udev/rules.d/`
free for your own overrides — and reloads udev, so the rule applies without
replugging. Removing the package reverses both, handing MTP back to your desktop
immediately.

The build must run on a Debian-derived system: the Go build is cgo against
libusb, so it cannot be cross-compiled from macOS without a cross toolchain.

### Anywhere else

Build the binary directly and install the rule by hand:

```sh
cd backend && go build -o /tmp/switchmtp ./cmd/switchmtp
sudo install -m 0755 /tmp/switchmtp /usr/local/bin/switchmtp
sudo cp packaging/linux/69-switchmtp.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules && sudo udevadm trigger
```

## `69-switchmtp.rules`

A udev rule granting the logged-in user access to a Nintendo Switch, and
stopping the desktop's own MTP handlers from claiming it first.

```sh
sudo cp 69-switchmtp.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules && sudo udevadm trigger
```

Then unplug and reconnect the Switch. No reboot or logout is needed.

### Why a udev rule is unavoidable

Two independent problems, both solved by this one file:

**`/dev/bus/usb` is root-only by default.** Without a rule granting access, the
Switch enumerates and is visible to `lsusb`, but `libusb_open()` fails with
`LIBUSB_ERROR_ACCESS`. This is the Linux equivalent of the USB permission prompt
on macOS.

**The desktop claims MTP devices automatically.** `gvfs-mtp` (GNOME),
`gvfs-gphoto2` and `kio-mtp` (KDE) mount any device tagged `ID_MTP_DEVICE` as
soon as a file manager notices it, and hold it open. libusb then fails with
`LIBUSB_ERROR_BUSY`. This is the exact same problem `ptpcamerad` causes on macOS,
under a different name.

The intuitive fix — `libusb_detach_kernel_driver()` — **does not work**, because
these are ordinary userspace processes, not kernel drivers. There is nothing to
detach. Killing them works but they respawn. Clearing `ID_MTP_DEVICE` so they
never recognise the device in the first place is the durable fix, and it is
scoped to Nintendo's vendor ID so every other MTP device on the system keeps
working normally.

### Distribution notes

The rule sets both `TAG+="uaccess"` and `GROUP="plugdev"` deliberately:

| | Mechanism that applies |
| --- | --- |
| Fedora, Arch, SteamOS | `uaccess` — these have no `plugdev` group |
| Debian, Ubuntu, Mint | `plugdev` (`uaccess` also works on a local session) |
| SSH / headless | `plugdev` only — `uaccess` grants access to the *seat*, and an SSH session is not one |

Whichever is meaningful on a given system takes effect; the other is inert.

### Steam Deck

`/etc/udev/rules.d/` is writable and **survives SteamOS updates**, even though
the rest of the root filesystem is immutable and reset on update. So the rule
only needs installing once. The `deck` user has passwordless `sudo` by default.

### This cannot ship inside a Flatpak or AppImage

A sandboxed package cannot write to `/etc/udev/rules.d/`, and udev is a host
service — there is no in-sandbox equivalent. However SwitchMTP is eventually
distributed, first-run setup will include this one-time step with `sudo`. Native
packages (`.deb`, `.rpm`, AUR) can install it from a post-install hook.

## Removing it

```sh
sudo rm /etc/udev/rules.d/69-switchmtp.rules
sudo udevadm control --reload-rules && sudo udevadm trigger
```

Normal desktop MTP behaviour returns immediately.
