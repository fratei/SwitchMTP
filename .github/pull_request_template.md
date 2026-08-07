<!--
Thanks for sending a patch. This template is short on purpose — every question
below exists because leaving it out has caused a problem before.
-->

## What this changes

<!-- One or two sentences. If it fixes a reported issue, write "Fixes #123". -->

## Why

<!--
The reasoning, not the diff. If this changes behaviour that looked deliberate,
explain why the old behaviour was wrong — several things in SwitchMTP look like
bugs and are not. CONTRIBUTING.md lists them.
-->

## How it was verified

Tick what actually happened. An honest "not tested on hardware" is far more
useful than an optimistic tick.

- [ ] `cd backend && go test ./...` passes
- [ ] `python3 -m pytest scripts/triage/test_triage.py -q` passes (if triage changed)
- [ ] `./scripts/build-app.sh` succeeds
- [ ] Tested against a real console running DBI's MTP responder
- [ ] Not tested against hardware — see below for why, and what should be checked

<!--
If you tested on hardware, say what: console model, HOS version, DBI version,
which mode DBI was in, macOS version, and which operations you exercised.

CI has no console attached. It compiles the app and runs the backend tests
against `backend/fake`, and that is all it can prove.
-->

## Anything a reviewer should look at closely

<!--
Uncertainty, trade-offs, things you were not sure about. Optional, but the most
valuable box on this form when it is filled in.
-->

---

- [ ] I have read [CONTRIBUTING.md](../CONTRIBUTING.md)
- [ ] `CHANGELOG.md` is updated under `## [Unreleased]`, or this change is invisible to users
- [ ] This work is contributed under GPL-2.0
