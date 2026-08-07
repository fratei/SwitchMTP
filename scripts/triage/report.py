"""Rendering the triage comment.

Kept separate from the rules so the wording can be tested, and so it is obvious
at a glance exactly what the bot will say on a stranger's issue.
"""

from __future__ import annotations

from forms import ParsedIssue
from rules import MARKER, Verdict

REPO_URL = "https://github.com/fratei/SwitchMTP"

_INTRO = {
    "needs-info": (
        "Thanks for the report. Before this can go anywhere, a few details are "
        "missing:"
    ),
    "answered": (
        "Thanks for the report. This matches something that is already "
        "understood and documented:"
    ),
    "duplicate": "Thanks for the report. This looks like it has already been raised.",
    "pr-recommended": (
        "Thanks — this is a complete report. It has been queued for a fix "
        "attempt."
    ),
    "needs-human": (
        "Thanks for the report. Automated triage could not resolve this one, so "
        "it has been flagged for a maintainer."
    ),
}


def doc_link(path: str) -> str:
    return f"{REPO_URL}/blob/main/{path}" if path else ""


def render(verdict: Verdict, parsed: ParsedIssue, *, dry_run: bool = False) -> str:
    lines: list[str] = [MARKER, ""]
    lines.append(_INTRO[verdict.kind])
    lines.append("")

    if verdict.kind == "needs-info":
        for item in verdict.missing:
            lines.append(f"- {item}")
        lines.append("")
        lines.append(
            "Add them in a comment and triage will pick the issue back up on its "
            "next run — there is no need to open a new issue."
        )
        if verdict.matches:
            lines.append("")
            lines.append("While you are here, one of these may already explain it:")
            lines.append("")
            for known in verdict.matches:
                link = doc_link(known.doc)
                lines.append(f"- {known.title}" + (f" — [read more]({link})" if link else ""))

    elif verdict.kind == "answered":
        for known in [k for k in verdict.matches if k.confidence == "high"]:
            lines.append(f"### {known.title}")
            lines.append("")
            lines.append(known.answer)
            link = doc_link(known.doc)
            if link:
                lines.append("")
                lines.append(f"More detail: [{known.doc}]({link})")
            lines.append("")
        lines.append(
            "If that does not describe your situation, say so here — automated "
            "triage gets things wrong and a correction is genuinely useful."
            + (
                " The issue will be reopened."
                if verdict.should_close
                else " This issue has been left open in the meantime."
            )
        )

    elif verdict.kind == "duplicate":
        lines.append(
            f"It reads as the same problem as #{verdict.duplicate_of}. Please follow "
            f"that issue for updates, and add anything your report has that it does "
            f"not — a different console, a different cable or a different macOS "
            f"version all help narrow a bug down."
        )
        lines.append("")
        lines.append(
            "If it is genuinely a different problem, say so and this will be reopened."
        )

    elif verdict.kind == "pr-recommended":
        lines.append(
            "Nothing further is needed from you for now. If a fix attempt produces "
            "a pull request it will be linked here, and you may be asked to try a "
            "build from it."
        )

    else:  # needs-human
        lines.append(
            "No further action is needed from you right now. If more information "
            "turns out to be needed, it will be asked for here."
        )

    lines.append("")
    lines.append("<details>")
    lines.append("<summary>How this was classified</summary>")
    lines.append("")
    lines.append(f"**Verdict:** {verdict.label}")
    lines.append("")
    if verdict.reasons:
        for reason in verdict.reasons:
            lines.append(f"- {reason}")
        lines.append("")
    if verdict.add_labels:
        lines.append("**Labels applied:** " + ", ".join(f"`{l}`" for l in verdict.add_labels))
        lines.append("")
    if parsed.form is not None:
        lines.append(f"**Form used:** `{parsed.form.filename}`")
    else:
        lines.append("**Form used:** none — the body was not a filled-in issue form")
    lines.append("")
    lines.append(
        "This comment was written by a rule-based script "
        f"([`scripts/triage`]({REPO_URL}/tree/main/scripts/triage)), not by a "
        "language model. It re-runs daily and edits this comment in place rather "
        "than adding new ones. If it got this wrong, say so — the rules are in "
        "the repository and can be corrected."
    )
    lines.append("")
    lines.append("</details>")

    if dry_run:
        lines.append("")
        lines.append("_(dry run — no labels or state were changed)_")

    return "\n".join(lines).rstrip() + "\n"
