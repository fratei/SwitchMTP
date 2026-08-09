# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Fixed

- **The Mac could fall asleep part way through a transfer.** Installing a queue of games
  runs for hours with the user deliberately away, and nothing was keeping the machine
  awake: the app took no power assertion at all, so it depended on some other process
  happening to hold one. Sleeping mid-transfer does not just pause it, it leaves the
  console holding a half-written title. The app now holds an assertion for exactly as long
  as a transfer is in flight, the same thing Finder does to copy a file. The display is
  still allowed to sleep, which is what an unattended transfer should permit.

- **The log did not record whether the app noticed a stalled transfer.** Stall detection
  reached the interface but never the log, so a log attached to a bug report showed a byte
  counter creeping forward and gave no way to answer the first question anyone would ask.
  Entering and leaving a stall are now both recorded, with how long the transfer had been
  idle and where it had got to. They are written as transitions rather than as a field on
  every progress line, so the moment it changed is not buried under hundreds of identical
  lines.

- **The diagnostic log could not tell two files apart.** It recorded the name of the file
  being sent through a helper that clipped it to 50 characters for display, and clipped the
  end — which is exactly where a Switch title carries its title id and version. A game and
  its own update therefore appeared in the log under one identical, truncated name, in the
  one artefact used to work out what went wrong after a failed transfer. The log now records
  the full name. The transfer bar is unaffected: it already shortened long names by cutting
  the middle, which keeps both ends readable, so removing the clip improves what is shown
  there too.

- **A long install queue could not be scrolled.** The queue list was a plain stack with no
  height limit, so queueing dozens of titles grew the transfer bar until it ran off the
  bottom of the window, taking the rows at the end with it and leaving no way to reach
  them. Past eight items the list is now pinned to a fixed height and scrolls, and it
  follows the queue as it advances so the title actually being sent stays in view. Shorter
  queues are still laid out at their natural height rather than sitting in a mostly empty
  scroll area.

- **A long install queue made the app burn CPU during transfers.** Every queue row was
  rebuilt ten times a second — once per progress update, for every row — because the row
  carried a closure, and a closure cannot be compared, so SwiftUI had to assume the row had
  changed. Each rebuild recreated a localised tooltip that in most cases was never shown;
  tooltip construction alone accounted for roughly half of each row's render cost. Rows are
  now compared on their contents, so a row is only rebuilt when it actually changes, and
  only the visible portion of a long queue is built at all.

- **A wedged install showed a frozen percentage and no error.** A 3.24 GB title reached 37%
  at a healthy 20 MB/s, then the console stopped draining the USB endpoint. Nothing failed:
  the host stayed blocked inside a libusb write that never returned, so no error was raised
  and no progress callback ever fired again. The app sat on the last percentage it had seen
  for hours, and the log recorded no error of any kind.

  Two things were missing, and the second is the one that actually mattered. A watchdog now
  keeps emitting progress while the byte counter stands still, so a total freeze is visible
  instead of silent. More importantly, movement is now judged by *rate* rather than by
  whether the number changed at all: the console in question was still accepting exactly
  one 16 KiB packet every 80 seconds, which satisfies any "has it moved?" check while being
  about 102 days from finishing. Below 64 KiB/s — some 300 times slower than a healthy
  transfer — the transfer is reported as stalled, with advice to check the console, since
  the host cannot tell an error dialog from a full SD card from applet mode running out of
  memory. Nothing is cancelled automatically; a stall is surfaced, not acted on.

- **Debian package versions could outrank real releases.** `build-deb.sh` decided whether a
  version string needed a `0.0.0+` prefix by checking whether it started with a digit — but
  a commit hash such as `9518481` does, so it was used verbatim, and dpkg ranks `9518481`
  above `1.0.0`. An untagged development build would therefore block upgrading to an actual
  release, and because it depended on the hash it happened for roughly two builds in three.
  The decision is now made on whether git found a tag at all. Builds past a tag are
  versioned `1.2.3+4.gabc1234`, which dpkg sorts above `1.2.3` and below `1.2.4`.

- **`switchmtp rm sdcard:/game.nsp --yes` no longer fails with `"--yes" is not a device
  path`.** Go's flag package stops parsing at the first argument that is not a flag, so
  any global flag typed after the subcommand was handed to the command as though it were a
  file name — and `--yes` after the path is how most people would type it. This was the
  first thing to go wrong when the tool was driven against a real console. Flags are now
  accepted anywhere on the line, and a `--` separator still means everything after it is a
  path, so files whose names begin with a dash remain reachable.

### Added

- **Releases now carry Linux artifacts.** Tagging builds the `.deb` for amd64 and arm64
  alongside the macOS DMG, plus a plain `.tar.gz` for distributions with neither package
  format. The Linux jobs run *before* the release is created rather than uploading to it
  afterwards, so a release is never visible in a half-populated state. The tarball's binary
  is extracted from the `.deb` rather than compiled a second time, making the two provably
  identical builds rather than merely equivalent ones.

- **An AUR `PKGBUILD`, covering Arch, Manjaro and SteamOS.** Published as `switchmtp-git`
  rather than `switchmtp`, because the project has no tagged releases yet and pinning a
  PKGBUILD to a tag that does not exist produces a package nobody can build; the file
  documents what to change when the first tag lands. Installs the same binary and udev
  rule as the `.deb`. Its build recipe was run against a real console rather than assumed.

- **A Debian package, so Linux installation is one command.** `scripts/build-deb.sh`
  produces a `.deb` carrying the `switchmtp` binary and the udev rule. A package is the
  right shape for this on Linux for a specific reason: it can install the udev rule, and a
  Flatpak or an AppImage cannot — that rule is the difference between the tool working and
  the desktop claiming the console first. Installing reloads udev so the rule applies
  without replugging, and removing it hands MTP back to the desktop immediately. Built and
  installed on Ubuntu 24.04 arm64 against a real console, including verifying that removal
  restores the desktop's own MTP support.

- **CI now builds and smoke-tests the Linux CLI on arm64 as well as amd64.** The arm64 leg
  is not speculative: it is the architecture the tool has had the most hardware validation
  on, having been built from a clean clone and driven against a real console on Ubuntu
  24.04 arm64, and it was the one architecture CI did not cover. Both binaries are
  published as artifacts.

- **Documented that `switchmtp` and your file manager cannot share the console.** On Linux
  the desktop's own MTP support already browses, reads and writes the Switch with no setup,
  which is worth knowing before installing anything. The two cannot be used in the same
  session though: once `switchmtp` has touched the device, GNOME's cached `gvfs` mount goes
  stale and every write through the file manager fails almost immediately with `libmtp
  error: Could not send object info`. Nothing is lost and a remount fixes it, but it was
  surprising enough to deserve writing down, along with the note that installing the udev
  rule avoids it entirely. Verified against Ubuntu 24.04 with a Switch running DBI.

- **Tests for the Linux process scanner, which had none that meant anything.** The scanner
  that finds gvfs or kio-mtp holding the device reads `/proc`, so on a CI runner with no
  USB devices it took no interesting branch and passed regardless of whether it worked.
  Its logic is now separated from the two paths that are genuinely Linux-specific and runs
  against a synthetic `/proc` fixture, so blocker matching, self-exclusion, unreadable
  processes belonging to other users, missing `comm` files and lookalike device paths are
  all exercised on every platform. Three latent bug classes in the blocker table are also
  pinned: a key with any uppercase character could never match, since lookup lowercases
  first; an entry longer than the 15 characters `/proc/<pid>/comm` reports needs a
  truncated variant or it never fires; and an entry without advice marks a process as to
  blame while saying nothing about what to do.
- **Cross-language drift tests.** A handful of facts exist in both Go and Swift with
  nothing connecting them at build time: the set of installable file types, and the error
  kind the app string-matches to detect a cancelled transfer. The second is the dangerous
  one — that match is what triggers resetting the MTP session after a cancel, and renaming
  the Go constant would compile, pass every other test, and leave the app wedged after the
  first cancelled transfer. Both are now pinned by tests that read the Swift sources and
  fail with an explanation of what will break.
- **A cross-platform `switchmtp` command line tool.** The existing `switchmtp-cli` is
  Swift and macOS-only; this one is Go, links `backend/nxmtp` directly with no FFI, and
  runs on Linux as well as macOS. It covers `devices`, `info`, `storages`, `ls`, `get`,
  `put`, `install`, `mkdir`, `rm`, `mv` and `doctor`, with `--json` output and exit codes
  that distinguish "no device", "cancelled", "device busy" and "bad usage" so scripts can
  react. Device paths are written `<storage>:<path>`, where the storage part accepts a
  numeric id, a friendly name such as `sdcard`, or a unique prefix of the display name;
  an ambiguous name is refused with the candidate ids rather than guessed, because
  guessing wrong could mean writing to NAND instead of the SD card. `Ctrl-C` cancels the
  in-flight MTP operation and closes the session cleanly instead of killing the process
  mid-transaction. CI builds it on Linux, smoke-tests it and publishes the binary as an
  artifact.
- **`switchmtp doctor` reports the Linux udev rule.** It looks for the rule, recognises an
  equivalent one installed by another Switch tool rather than demanding a redundant
  second copy, and prints the exact rule to install when there is none. A test pins that
  printed rule against the packaged `packaging/linux/69-switchmtp.rules` line by line, so
  the two cannot drift apart.
- **The Go backend now builds and is tested on Linux.** Nothing user-visible changes on
  macOS, but the MTP engine was only *theoretically* portable: it would not compile
  anywhere else, because `CollectDiagnostics()` existed solely in a cgo/IOKit file yet was
  called unconditionally, and the libusb cgo directives were macOS-only. Both are fixed,
  and CI now builds and runs the full Go test suite on `ubuntu-latest`. Since the device
  emulator in `backend/fake` needs no hardware, that suite is meaningful from the first
  run — so a macOS-only assumption creeping back into the backend now fails CI in under a
  minute instead of being discovered during a port.
- **Diagnostics understand Linux's version of the `ptpcamerad` problem.** macOS has
  `ptpcamerad` grabbing the Switch; Linux has `gvfs-mtp`, `gvfs-gphoto2` and `kio-mtp`
  doing exactly the same thing. On Linux the app now finds them by scanning `/proc` for
  processes holding a `/dev/bus/usb` descriptor, and names them with specific advice. The
  intuitive fix — `libusb_detach_kernel_driver()` — does not work on these, because they
  are userspace processes rather than kernel drivers; the rule below is the durable fix.
- **A udev rule for Linux**, at `packaging/linux/69-switchmtp.rules`, with an explanation
  of why it is unavoidable. It grants USB access (via both `uaccess` and `plugdev`, so it
  works on Fedora/Arch/SteamOS *and* Debian/Ubuntu *and* over SSH) and clears
  `ID_MTP_DEVICE` for Nintendo's vendor ID so the desktop's MTP handlers never claim the
  Switch. Scoped to Nintendo hardware, so every other MTP device on the system is
  unaffected.
- **Tests for the diagnostics advice.** Previously untested. The priority order is the
  part that matters — a Switch in DBIbackend/Awoo/GoldLeaf mode must be diagnosed as
  "wrong USB mode" and never as "no Switch detected", and "we cannot see USB at all" must
  stay distinct from "USB works, the Switch is not there". Those two pairs send users
  down opposite paths. Now pinned, and they run on both platforms.
- **Tests for the new CLI, run against the emulated device.** Every command is exercised
  end to end — argument parsing, storage resolution, the `nxmtp` call and the output
  formatting — with only the USB layer replaced by `backend/fake`. That includes a full
  `put` → `ls` → `get` round trip verifying the bytes survive. The whole suite therefore
  runs on a Linux CI runner that has never seen a Switch, which is the point: it is what
  turns "the backend should be portable" into evidence.

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

- **A crash when no USB devices are present at all.** The vendored libusb wrapper frees its
  device list by taking the address of the list's first element, which panics outright when
  the list is empty. A Mac always has at least a root hub so this never fired here, but it
  is reachable on any machine with no USB host controller — and it brought down the Go test
  suite the first time that suite ran on a Linux CI runner. The guard now lives in the
  library rather than at each call site, so every caller benefits.

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

- **Going to a folder could drop the USB connection for no reason — including
  during a copy.** "Go to folder" and the Favourites list treat an error appearing
  just after the jump as proof the folder is missing, and recover by disposing the
  MTP session and reconnecting. But the check looked at whether *any* error was on
  screen, not whether this navigation caused one, so a message the user simply had
  not dismissed yet was enough to tear down a healthy connection. Worse, it did not
  exclude transfers: a jump made while copying was deferred rather than attempted,
  so nothing had failed, yet an unrelated error in the same half-second would
  dispose the session and abort the copy. Navigation during a transfer no longer
  runs the recovery at all, and the check now only reacts to errors this navigation
  actually produced.

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
