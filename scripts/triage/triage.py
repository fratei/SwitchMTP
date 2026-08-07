#!/usr/bin/env python3
"""Triage SwitchMTP issues.

Run against one issue:

    python scripts/triage/triage.py --repo fratei/SwitchMTP --issue 12 --dry-run

Run against everything that needs a look (what the daily workflow does):

    python scripts/triage/triage.py --repo fratei/SwitchMTP --all

Nothing here needs a secret beyond a token that can read and label issues, and
`--dry-run` prints exactly what would happen without touching the repository.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Sequence

sys.path.insert(0, str(Path(__file__).resolve().parent))

import yaml  # noqa: E402

import report  # noqa: E402
from forms import (  # noqa: E402
    IssueForm,
    apply_followups,
    load_forms,
    parse_issue,
    validate_forms,
)
from gh import GitHub, token_from_env  # noqa: E402
from rules import (  # noqa: E402
    MARKER,
    IssueRef,
    Verdict,
    evaluate,
    load_known_issues,
)

ROOT = Path(__file__).resolve().parents[2]
TEMPLATE_DIR = ROOT / ".github" / "ISSUE_TEMPLATE"
KNOWN_ISSUES = Path(__file__).resolve().parent / "known_issues.yml"

#: Issues carrying any of these are considered settled; the bot leaves them be.
HANDS_OFF = {
    "status:confirmed",
    "status:in-progress",
    "status:wontfix",
    "status:by-design",
    "status:blocked-upstream",
    "triage:delegated",
    # Set when a reporter rejects a canned answer. Without it the next daily
    # pass would match the same rule, post the same answer and quietly erase the
    # hand-off to a human.
    "triage:disputed",
    "help wanted",
    "good first issue",
}

#: Replaces the triage comment once its conclusion has been rejected. Leaving
#: the original in place would keep asserting something the reporter has already
#: said is wrong.
DISPUTED_NOTE = """\
{marker}

You said the automated conclusion above did not fit, so this issue is now with \
a maintainer.{reopened}

Automated triage will not comment on it again — the `triage:disputed` label \
keeps it out of the bot's way. Nothing further is needed from you unless a \
maintainer asks.
"""

DELEGATION_INSTRUCTIONS = """\
This issue was routed here by SwitchMTP's automated triage because the report \
is complete and reproducible.

Before changing anything, read `docs/TROUBLESHOOTING.md` and \
`docs/HARDWARE_VALIDATION.md`. Several behaviours that look like bugs are \
deliberate: DBI reports no timestamps, sizes at or above 4 GiB are genuinely \
unknown, install storages are write-only, and rename is disabled because DBI \
does not implement `SetObjectPropValue`.

Two rules that are easy to break by accident:

* `claimWithRetry()` in `third_party/go-mtpfs/mtp/mtp.go` resets the device and \
claims it immediately. Do not add a delay there — it was measured, and waiting \
makes the claim fail.
* Device discovery must never open or claim a candidate just to describe it. \
Doing so triggers a reset storm.

There is no console attached to CI, so every change must be covered by unit \
tests against `backend/fake`. If the fix cannot be verified without hardware, \
say so in the pull request rather than guessing.
"""


def load_known(path: Path) -> list[Any]:
    data = yaml.safe_load(path.read_text(encoding="utf-8")) or []
    return load_known_issues(data)


def existing_triage_comment(comments: Sequence[dict[str, Any]]) -> dict[str, Any] | None:
    for comment in comments:
        if MARKER in (comment.get("body") or ""):
            return comment
    return None


def followup_bodies(
    comments: Sequence[dict[str, Any]], author: str
) -> list[str]:
    """Comments that may legitimately complete the report.

    Restricted to the person who opened the issue. Anything the bot wrote is
    excluded outright: its needs-info list quotes the field labels back, and
    reading that as an answer would let the bot satisfy itself.
    """
    out: list[str] = []
    for comment in comments:
        body = comment.get("body") or ""
        if MARKER in body:
            continue
        user = comment.get("user") or {}
        if user.get("type") == "Bot":
            continue
        if author and user.get("login") != author:
            continue
        out.append(body)
    return out


def to_ref(issue: dict[str, Any]) -> IssueRef:
    return IssueRef(
        number=int(issue["number"]),
        title=str(issue.get("title") or ""),
        body=str(issue.get("body") or "")[:4000],
        labels=tuple(l["name"] for l in issue.get("labels") or []),
    )


def closed_by_a_human(
    issue: dict[str, Any], comments: Sequence[dict[str, Any]]
) -> bool:
    """Was this issue closed by someone other than whoever runs triage?

    A maintainer closing an issue is the end of the conversation, and a bot
    that reopens it the next morning is worse than no bot at all. The bot's
    own identity is read off its marker comment rather than hard-coded, so
    this holds whether triage runs as `github-actions[bot]` in CI or as a
    person from a laptop.
    """
    if issue.get("state") != "closed":
        return False
    closer = ((issue.get("closed_by") or {}).get("login")) or ""
    if not closer:
        # Nothing to go on: assume a human, because the cost of wrongly
        # reopening a settled issue is higher than of leaving one closed.
        return True
    for comment in comments:
        if MARKER in (comment.get("body") or ""):
            bot = ((comment.get("user") or {}).get("login")) or ""
            return closer != bot
    return True


def disputed_answer(
    issue: dict[str, Any], comments: Sequence[dict[str, Any]]
) -> bool:
    """Did a human push back on the bot's conclusion?

    Both the "answered" and "duplicate" comments invite a correction, and an
    invitation the bot ignores is worse than no invitation at all. Any human
    comment after the bot's own counts: someone taking the trouble to reply to a
    canned answer is not agreeing with it, and a human glancing at a wrongly
    escalated issue is a far cheaper mistake than a real defect left buried
    under a wrong answer.
    """
    labels = {l["name"] for l in issue.get("labels") or []}
    if not labels & {"triage:answered", "duplicate"}:
        return False

    marker_seen = False
    for comment in comments:
        body = comment.get("body") or ""
        if MARKER in body:
            marker_seen = True
            continue
        if not marker_seen:
            continue
        if (comment.get("user") or {}).get("type") == "Bot":
            continue
        return True
    return False


def triage_one(
    api: GitHub,
    issue: dict[str, Any],
    *,
    forms: Sequence[IssueForm],
    known: Sequence[Any],
    others: Sequence[IssueRef],
    current_version: str,
    force: bool = False,
    can_delegate: bool = True,
) -> Verdict | None:
    number = int(issue["number"])
    labels = [l["name"] for l in issue.get("labels") or []]

    if not force and set(labels) & HANDS_OFF:
        print(f"#{number}: skipped, carries {sorted(set(labels) & HANDS_OFF)}")
        return None

    comments = api.comments(number)

    # List endpoints omit `closed_by`, and without it a genuine dispute could
    # never reopen anything. Only closed issues need the extra request.
    if issue.get("state") == "closed" and not issue.get("closed_by"):
        try:
            issue = api.issue(number)
            labels = [l["name"] for l in issue.get("labels") or []]
        except Exception:
            pass

    # A human replying to the bot's conclusion is disputing it. Honour that
    # before anything else: reopen if it was closed, hand it to a person, and
    # stop — re-running the rules would only reach the same wrong conclusion.
    if disputed_answer(issue, comments):
        # A maintainer's close outranks the bot's own. If a person closed this,
        # the objection still deserves a human's attention, but reopening it
        # would be the bot overruling the maintainer — so it stays closed and
        # is merely flagged.
        human_close = closed_by_a_human(issue, comments)
        was_closed = issue.get("state") == "closed"
        reopen = was_closed and not human_close
        if reopen:
            api.reopen_issue(number)
        api.add_labels(number, ["triage:disputed", "triage:needs-human"])
        for stale in ("triage:answered", "duplicate", "needs-info"):
            if stale in labels:
                api.remove_label(number, stale)
        previous = existing_triage_comment(comments)
        if previous is not None:
            api.update_comment(
                int(previous["id"]),
                DISPUTED_NOTE.format(
                    marker=MARKER,
                    reopened=(
                        " It has been reopened."
                        if reopen
                        else (
                            " It stays closed, because a maintainer closed it "
                            "deliberately rather than the bot — reopen it if you "
                            "think that was wrong."
                            if was_closed
                            else ""
                        )
                    ),
                ),
            )
        print(
            f"#{number}: disputed, handed to a maintainer"
            + ("" if not was_closed else " (left closed)" if not reopen else " (reopened)")
        )
        return None

    parsed = parse_issue(issue.get("body") or "", forms)
    author = ((issue.get("user") or {}).get("login")) or ""
    apply_followups(parsed, followup_bodies(comments, author))

    verdict = evaluate(
        title=issue.get("title") or "",
        body=issue.get("body") or "",
        parsed=parsed,
        existing_labels=labels,
        known_issues=known,
        open_issues=[o for o in others if o.number != number],
        current_version=current_version,
    )

    body = report.render(
        verdict, parsed, dry_run=api.dry_run, can_delegate=can_delegate
    )
    previous = existing_triage_comment(comments)

    if previous is None:
        api.create_comment(number, body)
    elif (previous.get("body") or "").strip() != body.strip():
        api.update_comment(int(previous["id"]), body)
    else:
        print(f"#{number}: comment unchanged")

    api.add_labels(number, verdict.add_labels)
    for label in verdict.remove_labels:
        api.remove_label(number, label)

    # Only close on the first pass. If a human reopened it, leave it alone.
    if verdict.should_close and previous is None and issue.get("state") == "open":
        api.close_issue(number, "completed")

    print(
        f"#{number}: {verdict.kind}"
        + (f" (duplicate of #{verdict.duplicate_of})" if verdict.duplicate_of else "")
        + f" +{verdict.add_labels or '[]'}"
        + (f" -{verdict.remove_labels}" if verdict.remove_labels else "")
    )
    return verdict


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY", ""))
    parser.add_argument("--issue", type=int, help="triage a single issue number")
    parser.add_argument("--all", action="store_true", help="triage every open issue")
    parser.add_argument(
        "--limit", type=int, default=40, help="cap on issues handled in one run"
    )
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument(
        "--force", action="store_true", help="ignore the hands-off labels"
    )
    parser.add_argument(
        "--check-templates",
        action="store_true",
        help="validate the issue forms and exit",
    )
    parser.add_argument(
        "--delegate",
        action="store_true",
        help="hand pr-recommended issues to the Copilot coding agent",
    )
    parser.add_argument("--output", help="write a JSON summary here")
    args = parser.parse_args(argv)

    problems = validate_forms(TEMPLATE_DIR)
    if problems:
        for problem in problems:
            print(f"::error::{problem}")
        return 1
    if args.check_templates:
        print(f"issue forms in {TEMPLATE_DIR.relative_to(ROOT)}: OK")
        return 0

    if not args.repo:
        parser.error("--repo is required (or set GITHUB_REPOSITORY)")
    if not args.issue and not args.all:
        parser.error("pass --issue N or --all")

    token = token_from_env()
    if not token:
        print("::error::no token in TRIAGE_TOKEN, GITHUB_TOKEN or GH_TOKEN")
        return 1

    api = GitHub(token, args.repo, dry_run=args.dry_run)
    forms = load_forms(TEMPLATE_DIR)
    known = load_known(KNOWN_ISSUES)
    current_version = api.latest_release_tag().lstrip("v")

    open_issues = api.open_issues()
    refs = [to_ref(i) for i in open_issues]

    if args.issue:
        targets = [api.issue(args.issue)]
    else:
        targets = [i for i in open_issues if not (set(
            l["name"] for l in i.get("labels") or []
        ) & HANDS_OFF)][: args.limit]

        # Closed issues the bot answered, touched in the last fortnight, so a
        # disputed close is picked up by the daily pass even if the webhook that
        # should have caught it was missed.
        since = (
            datetime.now(timezone.utc) - timedelta(days=14)
        ).strftime("%Y-%m-%dT%H:%M:%SZ")
        try:
            targets += [
                i
                for i in api.recently_closed_issues(since=since)
                if "triage:answered" in {l["name"] for l in i.get("labels") or []}
            ][: args.limit]
        except Exception as exc:  # a closed-issue sweep must never break the run
            print(f"::warning::could not list recently closed issues: {exc}")

    results: list[dict[str, Any]] = []
    delegate_to_copilot: list[int] = []

    # Settle up front whether a fix attempt can actually be handed to anything,
    # because the comment written for an actionable report says so either way.
    # A run that cannot delegate must not tell reporters their issue is queued.
    can_delegate = bool(args.delegate)
    if can_delegate:
        try:
            can_delegate = api.copilot_can_be_assigned()
        except Exception as exc:
            print(f"::warning::could not check for the Copilot agent: {exc}")
            can_delegate = False
        if not can_delegate:
            print("::notice::Copilot cannot be assigned here; issues will be "
                  "labelled for a maintainer instead of delegated")

    for issue in targets:
        try:
            verdict = triage_one(
                api,
                issue,
                forms=forms,
                known=known,
                others=refs,
                current_version=current_version,
                force=args.force or bool(args.issue),
                can_delegate=can_delegate,
            )
        except Exception as exc:  # keep going; one bad issue must not stop the run
            print(f"::warning::#{issue.get('number')} failed: {exc}")
            continue
        if verdict is None:
            continue
        results.append(
            {
                "number": int(issue["number"]),
                "title": issue.get("title"),
                "verdict": verdict.kind,
                "labels": verdict.add_labels,
                "duplicate_of": verdict.duplicate_of,
                "reasons": verdict.reasons,
            }
        )
        if verdict.kind == "pr-recommended":
            delegate_to_copilot.append(int(issue["number"]))

    delegated: list[int] = []
    if can_delegate:
        for number in delegate_to_copilot:
            if api.assign_copilot(number, DELEGATION_INSTRUCTIONS):
                api.add_labels(number, ["triage:delegated"])
                delegated.append(number)

    summary = {
        "repo": args.repo,
        "current_version": current_version,
        "considered": len(targets),
        "acted_on": len(results),
        "results": results,
        "delegated": delegated,
    }
    if args.output:
        Path(args.output).write_text(json.dumps(summary, indent=2), encoding="utf-8")

    step_summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if step_summary:
        with open(step_summary, "a", encoding="utf-8") as handle:
            handle.write(_markdown_summary(summary))

    print(json.dumps({k: v for k, v in summary.items() if k != "results"}, indent=2))
    return 0


def _markdown_summary(summary: dict[str, Any]) -> str:
    lines = ["## Issue triage", ""]
    if not summary["results"]:
        lines.append("Nothing needed attention.")
        return "\n".join(lines) + "\n"
    lines.append("| Issue | Verdict | Labels |")
    lines.append("| --- | --- | --- |")
    for row in summary["results"]:
        labels = ", ".join(f"`{l}`" for l in row["labels"]) or "—"
        lines.append(f"| #{row['number']} | {row['verdict']} | {labels} |")
    if summary["delegated"]:
        lines.append("")
        lines.append(
            "Delegated to the coding agent: "
            + ", ".join(f"#{n}" for n in summary["delegated"])
        )
    return "\n".join(lines) + "\n"


if __name__ == "__main__":
    raise SystemExit(main())
