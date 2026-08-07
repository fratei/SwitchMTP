# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

## [1.0.1] - 2026-08-07

First release validated against real hardware: a Nintendo Switch on HOS 22.5.0 running
DBI's MTP responder. Every fix below is a bug that only surfaced on a physical console.

### Fixed

- **Connecting on macOS at all.** macOS binds the Switch's still-image USB interface to its
  own `ptpcamerad` daemon before any application can claim it, so every connection failed
  with `LIBUSB_ERROR_ACCESS`. SwitchMTP now re-enumerates the USB port and re-claims the
  interface in the same instant, which wins the race against the daemon's automatic
  restart. It needs no privileges and works inside the App Sandbox.
- **Misleading connection errors.** A failed interface claim was silently ignored, so the
  real problem only surfaced later as an unrelated `LIBUSB_ERROR_NO_DEVICE`. Claim failures
  are now reported where they happen.
- **DBI being reported as stock Horizon OS.** DBI's MTP `DeviceInfo` says `Nintendo`/`Switch`,
  identical to stock firmware; it identifies itself only in its USB descriptors. Detection
  now uses those, so DBI is correctly recognised.
- **Every directory listing failing.** DBI advertises `GetObjectPropList` but rejects the
  request with `ParameterNotSupported` rather than `OperationNotSupported`, which bypassed
  the fallback. Both response codes now demote the capability and fall back to
  `GetObjectInfo`.
- **Sessions never opening.** The session was opened twice — once by device configuration and
  again by the client — and the second attempt is an error. Masked until connections started
  succeeding.
- **Stale sessions after a crash.** A session left open by a client that died mid-transaction
  is now closed and reopened on the post-reset path, not just the first attempt.
- **Diagnostics naming the wrong processes.** The occupancy check scanned every USB device on
  the machine, so it blamed web browsers and peripherals while missing the real culprit. It
  now inspects only the Switch, and only interface-level clients, which are the ones that can
  actually block a claim. `ptpcamerad` was additionally missed because the daemon name was
  recorded as `ptpcamera`.

### Added

- Regression test pinning the operation set reported by DBI on real hardware.
- Verified hardware results in `docs/HARDWARE_VALIDATION.md`, including DBI's supported and
  unsupported MTP operations.

## [1.0.0] - 2026-08-06

### Added

- Initial public release of SwitchMTP, a native macOS MTP client for Nintendo Switch devices running DBI's MTP responder.
- DBI-aware storage classification for SD Card, NAND, Saves, Album, Installed Games, Gamecard, and SD/NAND install targets.
- Device and operation capability probing with graceful degradation when optional MTP operations are unsupported.
- Drag-and-drop install targets for NSP, NSZ, XCI, and XCZ files, with progress reported for the upload step.
- Save backup and restore workflows, including dated backup folders.
- Album export and gamecard dump workflows.
- USB/MTP diagnostics for common connection, mode, and permission problems.
- Bundled `switchmtp-cli` for device listing, browsing, copying, installs, save backup, and diagnostics.
- Universal arm64 + x86_64 macOS build packaged as an ad-hoc signed DMG.

### Notes

- This release has not yet been validated against real Nintendo Switch hardware.
