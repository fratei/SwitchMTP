# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added

- **Hover explanations across the app.** Most controls had no tooltip, and the ones that
  did mostly repeated their own label. Toolbar buttons, breadcrumbs, sidebar storages, the
  install drop zone and the install queue now explain what they do — and, where a control
  is greyed out, *why*. A disabled button previously told the user only that something was
  unavailable, never that the fix was to select a file, connect a console, or wait for the
  current transfer; those three cases need different responses and now say so.
- **Tooltips in the file list.** The list is an `NSTableView`, so SwiftUI's `.help()` could
  never reach it: long filenames truncated mid-string with no way to read the rest. Name,
  date and kind cells now carry tooltips, column headers explain that clicking sorts, and a
  file whose date the device didn't report says so instead of showing a blank cell.
- **Settings ▸ General ▸ Write a diagnostic log**, with a button to reveal the log in
  Finder. Transfer failures were previously only diagnosable by knowing to run a
  `defaults write` command from Terminal, so in practice the log was never on when it was
  needed — including for the transfer failure that prompted adding it. The bug report form
  now points at the setting instead of the command.

### Changed

- **The bug report form takes the log and screenshots as file uploads** rather than asking
  for pasted text. Reporters had to open the log, select ~7 KB of it and paste it into a
  textarea, which meant most reports arrived without one. Both fields now use GitHub's
  upload widget with the file types each accepts, and the triage tooling validates the new
  field type. `docs/REPORTING_ISSUES.md` and `docs/TROUBLESHOOTING.md` were still teaching
  the old `defaults write` route and now describe the Settings toggle.

### Fixed

- **Browsing during a transfer froze the progress bar and hung the browser.** Navigating to
  another folder mid-copy left the progress bar stuck at whatever percentage it had reached
  and the file list spinning on "Loading files…" — while the copy itself carried on and
  finished perfectly well on the console. A 6.5 GB import reproduced it exactly: the bar
  stopped at 22%, 2,539 progress callbacks were discarded, and the completed transfer's
  result was handed to the directory-listing code, which could make no sense of it.

  Two separate faults, both now fixed. First, transfers shared a single-slot state machine
  with every other operation, so starting a directory listing overwrote the record that a
  transfer was in flight; every subsequent progress and completion callback was then routed
  to the wrong handler, or dropped. Transfers now own a slot of their own and report through
  their own completion callback, so no other operation can take their place. Second, MTP is
  a single session — the console genuinely cannot list a folder while it is copying, so the
  listing sat waiting for the transfer to release the connection. Rather than appear hung,
  SwitchMTP now says so and opens the folder the moment the transfer finishes.

- **Browsing was effectively dead for the length of an install queue.** Deferring a folder
  listing until the transfer finished was right for a single copy, but a queue of 22 games
  runs for hours and only leaves a third of a second between items, so in practice the file
  list stayed empty the whole time and refreshed only as one install handed over to the
  next. SwitchMTP now remembers the listing of every folder it has visited and shows it
  immediately, saying plainly that this is the folder as it was last seen and that it will
  refresh when the console is free. Every action that would change the console is already
  disabled during a transfer, so nothing can be done to a stale listing.

- **A second transfer started mid-copy jammed the queue permanently.** The toolbar's Import
  button correctly greys itself out during a transfer, but dragging files onto the window,
  dragging a file out of it, and double-clicking a file to preview it all reached the
  transfer code directly and bypassed that. Because MTP has only one session, the second
  copy overwrote the record of the first; whichever finished last found no record of itself
  and was discarded, so the app believed a transfer was still running forever and every
  remaining item in the install queue silently stopped. All five entry points now decline
  politely and explain why. Double-click preview in particular had stopped checking for
  transfers at all when transfers moved to their own state slot in the fix above — the most
  likely way to hit this, since it takes one stray double-click while browsing.

- **Pressing Connect with no console attached showed a raw parser error.** The button stays
  enabled with nothing plugged in — retrying after waking the console or starting the
  responder is the normal thing to do — but it passed an empty device id straight to the
  backend, which failed to parse it and put its own diagnostics on screen:
  `invalidInput: parseDeviceId: malformed device id ""`. That is not an answer to "why
  can't it see my Switch", and it was the first thing anyone opening the app before
  launching DBI would have seen. Connect now adopts a discovered console if one turned up
  after the last scan, and otherwise says what to check.
- **The file browser hung on "Loading files…" after connecting.** Clearing the operation
  state machine was moved into the same main-queue block as the follow-up directory walk,
  but it landed *after* the call that arms it. `loadFiles(at:)` claims the state machine by
  setting `operation = .walking`, so clearing it afterwards overwrote that claim, and the
  walk's completion callback was dispatched against `.none` — the listing was decoded,
  matched no case, and thrown away. The browser then span forever against a perfectly
  healthy console. The clear now happens before the walk is armed, at both sites.
- **File ▸ Import (and the rest of the File menu) was greyed out while the toolbar worked.**
  The File menu was built inline in the `App` struct's `.commands { }` from thirteen
  `@FocusedValue` properties. An `App` body is not re-evaluated when a focused value
  changes, so the menu's structural parts kept whatever value was current when the body last
  ran — it offered "Connect Device" against an already-connected console — while the
  sibling `.disabled()` modifiers saw fresh values. The commands now live in a
  `FileMenuCommands: Commands` type, which tracks focused values properly.
- **Every command in the Switch menu was permanently disabled.** Two causes. `MainView`
  published the manager with `.focusedValue`, which only resolves while the view holds
  keyboard focus — the file list takes it as soon as it appears — so the value was nil; it
  now uses `.focusedSceneValue` like every other key. And even once the manager arrived, the
  menu read `manager.connectionState` directly: `@FocusedValue` hands back the object but
  does not observe its `@Published` properties, so the menu never rebuilt on connect. The
  menu now reads an `Equatable` snapshot published from the view that does observe it.
  Help ▸ Report an Issue was affected by the same nil manager and silently omitted
  diagnostics from bug reports.

### Changed

- Uploads and the install queue now record their activity to the diagnostic log. The upload
  path had no logging at all — only downloads did — so a failed install left nothing behind
  to look at. Progress, the phase reported by the console, queue hand-offs and both classes
  of dropped callback are now traceable.

- **The interface is fully localised again.** 116 SwitchMTP-specific strings — the entire
  Switch menu, the install queue, storage capability errors, DBI setup guidance and the
  issue reporter — had only ever existed in English; every translated string in the catalog
  came from the upstream fork. They are now translated into Spanish, Japanese, Russian,
  Simplified Chinese and Traditional Chinese, and `scripts/apply-translations.py` fails if
  any key is left untranslated.
- Free-space labels in the sidebar are built from a single format string instead of gluing
  `"free of"` between two numbers, which could not be ordered correctly in every language.
- Removed the dead Apple Intelligence and AI-provider error messages inherited from the
  upstream fork. SwitchMTP has no AI features, so none of those errors could ever be
  produced; they were only inflating the translation surface.

- **Transfers were stuck on "Preparing transfer…" and never showed progress.** The Go
  backend reports `elapsedTime` as fractional seconds, but the Swift model declared it as
  `Int64`. `JSONDecoder` refuses to coerce `0.4231234` into an integer and rejects the
  *whole* payload, so every single progress update was discarded and the transfer
  statistics stayed empty for the entire transfer — even while DBI on the console visibly
  showed bytes arriving. Verified against a real console: 0 of 15 captured progress
  payloads decoded before this fix, 15 of 15 after.
- Progress decoding failures are now logged instead of being silently swallowed, so a
  future schema drift is visible rather than invisible.
- **Transfer speed was reported roughly a million times too high.** The backend emits
  bytes per second; the app labelled that figure "MB/s" without converting it.
- The file counter read "0 of 1 files" for the whole of a single-file transfer. It now
  counts the file being sent rather than the files already finished.
- An install that failed while waiting for the console to become ready left the transfer
  state marked as still running. It is now correctly reported as failed.
- A failed or cancelled file in a multi-file batch could leave the next file in the batch
  reporting the previous file's status.
- Installs are once again serialised against the console's readiness. The backend only
  waited for the Switch to answer again *between* files of one batch, so sending titles
  one at a time — which is what the queue does — skipped the wait entirely and could hand
  DBI a new title while it was still committing the previous one.
- A console unplugged mid-install no longer wedges the queue. The USB scan that detects
  the unplug resets the transfer state before the transfer's own callback arrives, so the
  running item could stay marked active forever with no way to clear it; every disconnect
  route now releases the queue, and reconnecting resumes anything still waiting.

### Added

- **An install queue.** Drop several NSP/NSZ/XCI/XCZ files — in one go or while an install
  is already running — and they are installed one after another, each starting
  automatically when the previous one finishes. The queue is visible, reorderable by
  removal, and individually cancellable.
- **The transfer UI is no longer a modal sheet.** It is now a bar at the bottom of the
  window, so the rest of the app — including the install drop targets — stays usable
  during a transfer. Queueing another install while one is running was previously
  impossible for exactly this reason.
- The app now reports the **install phase** distinctly from the transfer phase. DBI
  installs the title only after the last byte arrives and sends no completion event, so a
  transfer that reached 100% used to look wedged for minutes. The bar now says the console
  is installing, and keeps a live elapsed counter while it waits.
- A **stall notice** appears if no progress has been reported for 15 seconds, alongside a
  Cancel button that is always available.
- **Help ▸ Report an Issue…** opens a GitHub bug report with the app version and macOS
  version already filled in, and offers to copy a diagnostics report to the clipboard
  first.
- Structured issue forms for bug reports, feature requests and compatibility reports, so
  the details that actually determine whether a bug can be fixed — DBI's mode, the
  diagnostics report, reproduction steps — are asked for rather than hoped for.
- Automated issue triage (`scripts/triage`): a rule-based engine, with no model calls and
  no required secrets, that runs daily and on every issue event. It routes and labels
  reports, says precisely what is missing from an incomplete one, answers reports that
  match a documented behaviour, flags likely duplicates, and hands complete, reproducible
  reports to the Copilot coding agent when a token is configured.
- `CONTRIBUTING.md`, `SECURITY.md`, `docs/REPORTING_ISSUES.md` and a pull request
  template.

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
