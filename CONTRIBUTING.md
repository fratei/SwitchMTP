# Contributing to SwitchMTP

SwitchMTP talks to a homebrew MTP responder over USB on one operating system.
Almost every interesting bug lives in that intersection, and almost none of it
can be verified in CI. That shapes everything below.

---

## Reporting a bug

See **[docs/REPORTING_ISSUES.md](docs/REPORTING_ISSUES.md)**. The fastest route
is **Help ▸ Report an Issue…** in the app.

---

## Getting set up

```shell
git clone https://github.com/fratei/SwitchMTP.git
cd SwitchMTP
./scripts/bootstrap.sh      # checks Go, Xcode and XcodeGen, and says how to fix what is missing
./scripts/build-libusb.sh   # universal libusb, once
./scripts/build-app.sh      # backend + app, roughly three minutes
```

You need Go 1.21 or newer, **full Xcode** (not just the Command Line Tools) and
XcodeGen. Two environment traps catch everyone once:

```shell
export DEVELOPER_DIR="$(xcode-select -p)"          # before building the app
export SDKROOT="$(xcrun --sdk macosx --show-sdk-path)"  # before any cgo work
```

---

## Running the tests

```shell
cd backend && go test ./...                        # the Go backend, against the fake device
python3 -m pytest scripts/triage/test_triage.py -q # the issue-triage engine
```

`backend/fake` is an in-process emulator of DBI's responder: the same storages,
the same read-only and write-only semantics, the same missing optional
operations, the same 32-bit size overflow. **A backend change that cannot be
covered there needs a very good reason.** There is no console attached to CI, so
the fake is the only automated protection the project has.

---

## Things that look wrong and are not

Several behaviours get "fixed" by well-meaning changes and then have to be
reverted. Before changing any of these, read the reasoning:

- **`claimWithRetry()` in `third_party/go-mtpfs/mtp/mtp.go` resets the device
  and claims it in the same breath.** It looks like it is missing a settle
  delay. It is not: macOS restarts `ptpcamerad` immediately after the reset, and
  a 120 ms delay loses the race every single time it was measured. Zero delay
  wins every time. Do not add a sleep there.
- **Device discovery never opens or claims a candidate just to describe it.**
  Claiming triggers a reset, the reset triggers hotplug, hotplug triggers
  another scan. The result is a reset storm at roughly 1 Hz. Descriptions come
  from cached descriptor data only.
- **Dates show `—` rather than a date.** DBI reports no `ObjectInfo` timestamps
  at all. An earlier version substituted the current time, which made every file
  look freshly modified.
- **Sizes at or above 4 GiB show `—`.** MTP's `ObjectInfo` size field is 32 bits
  and overflows to `0xFFFFFFFF`. `GetObjectPropValue(ObjectSize)` is optional
  and DBI does not implement it everywhere. Showing "4 GB" would be a confident
  lie; transfers use the bytes that actually arrive.
- **Rename is disabled.** It needs `SetObjectPropValue`, which DBI does not
  advertise. The app disables operations the device says it cannot do instead of
  letting them fail half-way.
- **Install storages are not browsable.** They are write-only drop targets, and
  MTP gives no completion event for an install.

---

## Pull requests

- One logical change per pull request.
- `gofmt` for Go. Match the surrounding style in Swift; there is no formatter
  configured, deliberately.
- Comment *why*, not *what*. The rule of thumb used throughout this codebase is
  that a comment earns its place when the code below it would otherwise look
  like a mistake.
- Update `CHANGELOG.md` under `## [Unreleased]` for anything a user would
  notice.
- If a change can only be verified with a console attached, **say so in the pull
  request** and describe exactly what you tested and on what. Do not imply CI
  covered something it cannot cover.

The pull request template asks for this; it is short and it is not a formality.

---

## Licensing

SwitchMTP is GPL-2.0, inherited from
[Neighbor-Z/SwiftMTP](https://github.com/Neighbor-Z/SwiftMTP). By contributing
you agree your work is licensed the same way.

Third-party code lives in `third_party/` with its original licence intact and is
recorded in [`THIRD_PARTY.md`](THIRD_PARTY.md). **Do not vendor code without a
licence**, however convenient — that is precisely why the original
`go-mtpx` dependency was replaced rather than reused.

---

## Automated triage

Issues are processed by [`scripts/triage`](scripts/triage), a rule-based script
with no model calls. If it misclassifies something, the fix is a change to
`scripts/triage/known_issues.yml` or `rules.py` plus a test in
`test_triage.py` — which is a genuinely good first contribution, because it
needs no console.

Run it against a real issue without touching anything:

```shell
export TRIAGE_TOKEN="$(gh auth token)"
python3 scripts/triage/triage.py --repo fratei/SwitchMTP --issue 42 --dry-run
```
