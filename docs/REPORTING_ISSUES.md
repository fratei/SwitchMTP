# Reporting an issue

The short version: use **Help ▸ Report an Issue…** in the app. It opens the
right form with your version and environment already filled in, and offers to
put a diagnostics report on the clipboard for you to paste.

The rest of this page explains what the forms ask for, why, and what happens
to a report after you file it.

---

## Before you file

Two links resolve most reports on their own:

- **[Troubleshooting](TROUBLESHOOTING.md)** — the console not being detected,
  `Extra buffers exceeded`, exported files apparently vanishing, dates showing
  as `—`, Gatekeeper refusing to open the app.
- **[DBI setup](DBI_SETUP.md)** — if the console never appears at all, start
  here. The most common cause is DBI running in a USB mode that is not MTP.

If neither helps, file the report. A duplicate is a minor nuisance; a bug that
nobody mentions is a bug that never gets fixed.

---

## Which form to use

| Form | Use it when |
| --- | --- |
| **Bug report** | Something behaves differently from how it is documented or how it obviously should. |
| **Feature request** | You want SwitchMTP to do something it does not do. |
| **Compatibility report** | You tried a console, firmware, responder, cable or dock that may not have been tested. **Including when it all worked** — that is how the compatibility notes become trustworthy. |

Questions and half-formed ideas belong in
[Discussions](https://github.com/fratei/SwitchMTP/discussions) rather than in an
issue.

---

## The three things that decide whether a bug gets fixed

### 1. Steps somebody else can follow

Not "exporting is broken" but:

```
1. Launch DBI in title mode and choose "MTP responder"
2. Open SwitchMTP and wait for the console to appear
3. Select SD Card, open /switch
4. Select three .nro files and press Export
5. Choose ~/Desktop, press "Export Here"
6. Only the first file is written; no error appears
```

The second version can be reproduced on a different console. The first cannot.

### 2. The diagnostics report

**Switch ▸ Copy Diagnostics** in the app, or from a terminal:

```shell
/Applications/SwitchMTP.app/Contents/MacOS/switchmtp-cli --json doctor
```

It lists the USB devices present, which process is holding the interface, the
connected console's storages, and which optional MTP operations it advertises.
For anything to do with connecting or transferring, this turns guesswork into
fact.

It contains **no file contents and no save data**. It does include your macOS
version, the console's serial number, and the names of USB devices attached to
your Mac.

### 3. Which mode DBI was in

Applet mode — DBI launched from the album — gives homebrew a small memory
allocation, and large transfers fail because of it. Title mode means holding
**R** while starting an installed game. This one field explains a large share of
transfer reports, so the bug form asks for it and will not accept a report
without it.

---

## The verbose transfer log

Only worth collecting for transfer problems, because it is noisy. There is a
switch for it in the app:

1. **SwitchMTP ▸ Settings ▸ General ▸ Write a diagnostic log**.
2. Quit and reopen the app — the setting is read once at launch.
3. Reproduce the problem.
4. **Show Log in Finder**, on that same Settings screen.

Then drag `switchmtp-debug.log` straight into the **Transfer log** box on the
bug form. It is an upload field, so there is no need to open the file or paste
its contents.

Turn the setting off again afterwards; the log keeps growing while it is on.

If you would rather not use the UI, the same switch is a user default:

```shell
defaults write me.fratei.switchmtp debugLogEnabled -bool YES
# relaunch the app, reproduce the problem, then:
open ~/Library/Containers/me.fratei.switchmtp/Data/tmp/
# turn it off again:
defaults delete me.fratei.switchmtp debugLogEnabled
```

**The log records file and folder names from your console.** Have a look at it
before you attach it.

---

## Screenshots

Drag images straight into the screenshots box. A picture of the error, plus the
sidebar showing what the app detected, is often enough on its own. Short screen
recordings are accepted too — GitHub takes `.mp4` and `.mov` up to 10 MB.

---

## What happens next

Every issue is looked at by an automated triage pass — daily, and again whenever
an issue is opened, edited or commented on. It is a rule-based script
([`scripts/triage`](../scripts/triage)), not a language model, so it behaves the
same way every time and you can read exactly what it will do.

It posts **one** comment and keeps editing that same comment as things change,
rather than piling up new ones. It will do one of five things:

| Verdict | What it means |
| --- | --- |
| **More information needed** | Something required is missing. It says precisely what. Add it in a comment — no need to open a new issue — and the next pass picks it up. |
| **Answered** | The report matches something already documented. The answer is quoted in full with a link. Some of these close the issue; where the same symptom could also be a real defect, it stays open. |
| **Looks like a duplicate** | It reads as the same problem as an existing issue, which is linked. |
| **Actionable** | The report is complete and reproducible. If the repository is set up to delegate fixes, one has been queued; otherwise it is waiting for a maintainer. |
| **Needs a maintainer** | Triage could not classify it. A human will read it. |

It also applies `area:*` and `severity:*` labels so issues can be found later.

### Answering a request for more information

Reply in a comment on the same issue; there is no need to open a new one. The
parser is looking for the field label, so keep it visible — any of these work:

```
macOS version: 26.6

**Steps to reproduce**:
1. Launch DBI in title mode and pick MTP responder
2. Open SwitchMTP and wait for the console to appear
3. Select every file on the SD card and press Download
```

A one-line answer written next to the label is read as-is; a longer answer
written *underneath* the label is read down to the next field. If triage said an
answer you already gave was too thin, the new text is added to it — nothing you
wrote is replaced or thrown away.

**Triage gets things wrong, and it is built to be told so.** Reply to its
comment saying the answer does not fit. The next pass replaces its comment,
labels the issue `triage:disputed` and hands it to a human — and then leaves it
alone permanently, so it cannot talk itself back into the same wrong answer. The
same applies to a wrong duplicate call.

If the bot closed the issue, disputing it reopens it. If a *maintainer* closed
it, it stays closed: the objection is still flagged for a human, but the bot
does not overrule a person's decision.

Anything a maintainer has marked `status:confirmed`, `status:in-progress`,
`status:by-design`, `status:wontfix` or `status:blocked-upstream` is left alone
by the bot entirely. It never applies those itself — they record a human's
decision.

---

## Security

Do not report a security problem in a public issue. See
[SECURITY.md](../SECURITY.md).
