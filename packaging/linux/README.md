# Linux packaging

This directory holds the Linux-side assets for SwitchMTP. There is no Linux
application yet — the [cross-platform port](../../docs/) is in progress and the
Go backend is the part that exists so far. What is here is the piece that has to
be right before anything else can work: USB access.

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
