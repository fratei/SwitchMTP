# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

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
