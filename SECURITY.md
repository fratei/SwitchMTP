# Security policy

## Reporting a vulnerability

**Do not open a public issue.**

Use GitHub's private reporting:
[**Report a vulnerability**](https://github.com/fratei/SwitchMTP/security/advisories/new).
It is private to you and the maintainers until an advisory is published.

Please include what an attacker can achieve, the steps to reproduce it, and the
version you tested. A proof of concept is welcome but not required.

Expect an acknowledgement within a week. This is a spare-time project, so please
allow a reasonable window before disclosing publicly, and say up front if you
have a deadline in mind.

## Supported versions

Only the latest release. There are no maintenance branches.

## What is in scope

SwitchMTP parses data that comes off a USB device and writes files to your Mac.
Anything in that path is in scope:

- Memory-safety or parsing faults in the Go backend or the vendored MTP code
  reachable from a hostile or malformed device response.
- Path traversal when writing downloaded files — a device-supplied filename
  escaping the chosen destination directory.
- Anything that lets a USB device read or write outside the App Sandbox, or that
  escalates beyond the entitlements in `app/Resources/SwitchMTP.entitlements`.
- Leaking credentials or personal data through the diagnostics report or the
  debug log.

## What is not in scope

- **The ad-hoc code signature.** Releases are signed ad-hoc, not with a paid
  Developer ID, so Gatekeeper warns and you have to clear the quarantine
  attribute yourself. This is a documented, deliberate trade-off, not a
  vulnerability.
- **The USB port reset.** SwitchMTP resets the USB port to reclaim the still-image
  interface from macOS's own `ptpcamerad`. It is a documented, unprivileged
  operation available to any application, it affects only the port the console is
  attached to, and the app quits no processes on your Mac. See
  **Help ▸ How SwitchMTP connects** in the app.
- **Bugs in DBI, Atmosphère or Nintendo's firmware.** Report those upstream.
  Something in SwitchMTP that a hostile *device* can exploit is in scope; a flaw
  in the responder itself is not.
- **The debug log containing filenames.** It is off by default, it must be turned
  on with a `defaults write`, and both the app and the docs say what it records.

## What SwitchMTP does not do

Stated plainly, because these come up:

- It makes **no network connections at all**, other than the explicit update
  check against the GitHub releases API, which sends nothing but the request.
- It has no telemetry, no analytics and no crash reporting.
- It never quits or kills processes on your Mac.
- It stores no credentials.
