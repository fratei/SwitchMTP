"""The deterministic part of issue triage.

Everything here is a pure function of the issue's own content plus a small
knowledge base. There is no network access and no model call, which is the
point: the daily workflow has to produce the same verdict for the same issue
every time it runs, and it has to work on a public repository where no secrets
are available.

The engine answers four questions, in order:

1. Is the report complete enough to act on?
2. Does it match something we already know about and have documented?
3. Is it a duplicate of an open issue?
4. If none of the above, is it concrete enough that opening a pull request is
   the sensible next step?
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any, Iterable, Sequence

from forms import IssueForm, ParsedIssue, normalise_label

MARKER = "<!-- switchmtp-triage:v1 -->"

# --------------------------------------------------------------------- routing

#: Dropdown option text -> area label. Keyed on a normalised prefix so that
#: rewording the tail of an option does not silently break routing.
AREA_BY_OPTION = {
    "finding or connecting": "area:usb",
    "browsing files": "area:app",
    "downloading from the console": "area:transfer",
    "uploading from the mac": "area:transfer",
    "installing nsp": "area:transfer",
    "backing up or restoring saves": "area:saves",
    "album": "area:app",
    "renaming, deleting": "area:backend",
    "the command line tool": "area:cli",
    "the window, menus": "area:app",
    "building from source": "area:build",
}

#: Fallback routing for reports that did not use the form, or that left the
#: area dropdown empty. Ordered: the first match wins for the primary area, but
#: every match contributes a label.
AREA_BY_KEYWORD: Sequence[tuple[str, str]] = (
    (r"\b(usb|libusb|claim|enumerat\w+|hotplug|port reset|ptpcamera|image capture|"
     r"not detected|no device|does ?n.?t (show|appear)|0x?057e|vid|pid)\b", "area:usb"),
    (r"\b(upload|download|transfer|export|import|copy|progress|install\w*|nsp|nsz|xci|xcz|"
     r"stall\w*|resume|cancel\w*|speed|throughput)\b", "area:transfer"),
    (r"\b(save ?data|savedata|saves?|backup|restore)\b", "area:saves"),
    (r"\b(cli|command.?line|terminal|switchmtp-cli|--json)\b", "area:cli"),
    (r"\b(build|compil\w+|xcodebuild|xcodegen|go build|make|ci|workflow|signing|"
     r"notariz\w+|dmg|gatekeeper|quarantine)\b", "area:build"),
    (r"\b(readme|docs?|documentation|typo|spelling|wording|instructions)\b", "area:docs"),
    (r"\b(window|sidebar|menu|button|toolbar|column|dark mode|localis\w+|localiz\w+|"
     r"quick ?look|drag|drop|icon|layout|ui|interface)\b", "area:app"),
    (r"\b(nxmtp|ffi|dylib|go backend|goroutine|panic:|objectid|objectinfo|"
     r"getobjectprop\w*|storageid)\b", "area:backend"),
)

# ------------------------------------------------------------------- severity

DATA_LOSS = re.compile(
    r"\b(delet\w+ (my|all|the wrong)|lost (my )?(data|saves?|files?)|overwr\w+|"
    r"corrupt\w*|wiped|destroy\w+|truncat\w+|brick\w*|unrecoverable)\b",
    re.IGNORECASE,
)
CRASH = re.compile(
    r"\b(crash\w*|panic:|hang\w*|frozen|freezes?|freezing|force ?quit|"
    r"beach ?ball|spinning ?(beach ?ball|wheel)|unresponsive|deadlock|"
    r"not responding|SIGSEGV|SIGABRT|EXC_BAD_ACCESS)\b",
    re.IGNORECASE,
)
BLOCKING = re.compile(
    r"\b(completely (broken|unusable)|never works?|can'?t (use|connect|transfer) at all|"
    r"unusable|always fails?|fails? every time|no ?longer works?|regression)\b",
    re.IGNORECASE,
)
COSMETIC = re.compile(
    r"\b(typo|spelling|misspell\w*|wording|alignment|padding|tooltip|"
    r"capitali[sz]ation|cosmetic|nitpick|label (says|reads))\b",
    re.IGNORECASE,
)

VERSION_RE = re.compile(r"(\d+)\.(\d+)\.(\d+)")

STOPWORDS = {
    "a", "about", "after", "all", "also", "an", "and", "any", "app", "are", "as", "at",
    "be", "been", "but", "by", "can", "cannot", "could", "did", "do", "does", "doesn",
    "doing", "don", "either", "even", "every", "for", "from", "get", "gets", "got", "had",
    "has", "have", "how", "however", "if", "in", "into", "is", "isn", "it", "its", "just",
    "like", "make", "makes", "many", "may", "me", "more", "most", "much", "my", "no",
    "not", "now", "of", "off", "on", "one", "only", "or", "other", "our", "out", "over",
    "own", "same", "see", "seems", "should", "since", "so", "some", "still", "such",
    "than", "that", "the", "their", "them", "then", "there", "these", "they", "this",
    "those", "through", "to", "too", "try", "trying", "under", "until", "up", "use",
    "used", "using", "very", "want", "was", "wasn", "we", "well", "were", "what", "when",
    "where", "which", "while", "who", "why", "will", "with", "won", "would", "you",
    "your", "switchmtp", "switch", "issue", "bug", "problem",
}


@dataclass
class KnownIssue:
    id: str
    title: str
    confidence: str
    answer: str
    doc: str = ""
    close: bool = False
    labels: tuple[str, ...] = ()
    any_of: tuple[re.Pattern[str], ...] = ()
    all_of: tuple[re.Pattern[str], ...] = ()
    none_of: tuple[re.Pattern[str], ...] = ()
    fields: dict[str, str] = field(default_factory=dict)

    def matches(self, haystack: str, parsed: ParsedIssue) -> bool:
        if not self.any_of:
            return False
        if not any(p.search(haystack) for p in self.any_of):
            return False
        if not all(p.search(haystack) for p in self.all_of):
            return False
        if any(p.search(haystack) for p in self.none_of):
            return False
        for field_id, needle in self.fields.items():
            value = parsed.get(field_id, "").casefold()
            if needle.casefold() not in value:
                return False
        return True


def load_known_issues(data: Iterable[dict[str, Any]]) -> list[KnownIssue]:
    out: list[KnownIssue] = []
    for entry in data or []:
        match = entry.get("match") or {}

        def compile_all(key: str) -> tuple[re.Pattern[str], ...]:
            return tuple(
                re.compile(p, re.IGNORECASE) for p in (match.get(key) or [])
            )

        out.append(
            KnownIssue(
                id=str(entry["id"]),
                title=str(entry.get("title", entry["id"])),
                confidence=str(entry.get("confidence", "medium")).lower(),
                answer=str(entry.get("answer", "")).strip(),
                doc=str(entry.get("doc", "")),
                close=bool(entry.get("close", False)),
                labels=tuple(entry.get("labels") or []),
                any_of=compile_all("any_of"),
                all_of=compile_all("all_of"),
                none_of=compile_all("none_of"),
                fields={str(k): str(v) for k, v in (match.get("fields") or {}).items()},
            )
        )
    return out


@dataclass
class IssueRef:
    number: int
    title: str
    body: str = ""
    labels: tuple[str, ...] = ()


@dataclass
class Verdict:
    kind: str
    reasons: list[str] = field(default_factory=list)
    add_labels: list[str] = field(default_factory=list)
    remove_labels: list[str] = field(default_factory=list)
    missing: list[str] = field(default_factory=list)
    matches: list[KnownIssue] = field(default_factory=list)
    duplicate_of: int | None = None
    should_close: bool = False

    @property
    def label(self) -> str:
        return {
            "needs-info": "More information needed",
            "answered": "Answered from the troubleshooting guide",
            "duplicate": "Looks like a duplicate",
            "pr-recommended": "Actionable — a fix should be attempted",
            "needs-human": "Needs a maintainer to look",
        }[self.kind]


def searchable_text(title: str, parsed: ParsedIssue, body: str) -> str:
    """The text known-issue rules and keyword routing run against.

    The title plus the reporter's own prose, and nothing else.

    This is an allowlist rather than a denylist, because a denylist is only ever
    one new form field away from being wrong. Dropdowns hold a controlled
    vocabulary that reads like prose and matches keyword rules by accident — an
    install source of "DMG from the Releases page" routed a crash report to
    `area:build`. They already have precise routing of their own through
    `AREA_BY_OPTION` and the `fields:` constraints in the knowledge base, so
    they have no business here. Short inputs are version numbers.

    The pasted diagnostics and logs are excluded for the same reason at greater
    volume: they are full of device names and paths, and matching on them
    produces confident nonsense.
    """
    if parsed.form is None:
        return f"{title}\n{body}"

    # Dumps the reporter pasted rather than wrote.
    skip = {"diagnostics", "logs", "screenshots", "checks"}
    prose = {
        f.id
        for f in parsed.form.fields
        if f.type == "textarea" and f.id not in skip
    }
    parts = [title]
    for field_id, value in parsed.values.items():
        if field_id in prose:
            parts.append(value)
    return "\n".join(p for p in parts if p)


def route_areas(text: str, parsed: ParsedIssue) -> list[str]:
    areas: list[str] = []
    selected = parsed.get("area", "")
    if selected:
        for line in selected.split("\n"):
            key = normalise_label(line.lstrip("-* ").strip())
            for prefix, label in AREA_BY_OPTION.items():
                if key.startswith(prefix):
                    if label not in areas:
                        areas.append(label)
                    break
    if not areas:
        for pattern, label in AREA_BY_KEYWORD:
            if re.search(pattern, text, re.IGNORECASE) and label not in areas:
                areas.append(label)
    return areas[:3]


def classify_severity(text: str, parsed: ParsedIssue, is_bug: bool) -> str:
    if not is_bug:
        return ""
    if DATA_LOSS.search(text):
        return "severity:data-loss"
    if CRASH.search(text):
        return "severity:crash"
    if COSMETIC.search(text) and not BLOCKING.search(text):
        return "severity:low"
    frequency = parsed.get("reproducible", "").casefold()
    if BLOCKING.search(text) or frequency.startswith("every time"):
        return "severity:high"
    outcome = parsed.get("outcome", "").casefold()
    if outcome.startswith("the console was never detected"):
        return "severity:high"
    return "severity:medium"


def _looks_like_steps(text: str) -> bool:
    """Distinguish real reproduction steps from a one-line restatement."""
    lines = [l.strip() for l in text.split("\n") if l.strip()]
    numbered = sum(1 for l in lines if re.match(r"^(\d+[.)]|[-*+])\s+\S", l))
    if numbered >= 2:
        return True
    return len(lines) >= 2 and len(text) >= 60


def _version_is_current(reported: str, current: str) -> bool | None:
    """None when either version cannot be parsed."""
    a = VERSION_RE.search(reported or "")
    b = VERSION_RE.search(current or "")
    if not a or not b:
        return None
    return tuple(int(x) for x in a.groups()) >= tuple(int(x) for x in b.groups())


def tokenise(text: str) -> set[str]:
    words = re.findall(r"[a-z0-9]{3,}", (text or "").casefold())
    return {w for w in words if w not in STOPWORDS}


def similarity(a: str, b: str) -> float:
    ta, tb = tokenise(a), tokenise(b)
    if not ta or not tb:
        return 0.0
    return len(ta & tb) / len(ta | tb)


def find_duplicate(
    title: str,
    summary: str,
    others: Sequence[IssueRef],
    threshold: float = 0.55,
) -> tuple[int | None, float]:
    best_number: int | None = None
    best_score = 0.0
    mine = f"{title}\n{summary}"
    for other in others:
        score = max(
            similarity(title, other.title),
            0.9 * similarity(mine, f"{other.title}\n{other.body}"),
        )
        if score > best_score:
            best_number, best_score = other.number, score
    if best_score >= threshold:
        return best_number, best_score
    return None, best_score


def evaluate(
    *,
    title: str,
    body: str,
    parsed: ParsedIssue,
    existing_labels: Sequence[str] = (),
    known_issues: Sequence[KnownIssue] = (),
    open_issues: Sequence[IssueRef] = (),
    current_version: str = "",
) -> Verdict:
    existing = set(existing_labels)
    text = searchable_text(title, parsed, body)
    form_name = parsed.form.filename if parsed.form else ""

    is_bug = "type:bug" in existing or form_name.startswith("1-")
    is_feature = "type:feature" in existing or form_name.startswith("2-")
    is_compat = "type:compatibility" in existing or form_name.startswith("3-")
    if not (is_bug or is_feature or is_compat):
        # A hand-written issue. Guess, but say that we guessed.
        is_bug = True

    verdict = Verdict(kind="needs-human")

    # --- labels that apply regardless of the verdict -----------------------
    for area in route_areas(text, parsed):
        verdict.add_labels.append(area)
    severity = classify_severity(text, parsed, is_bug and not is_compat)
    if severity and not any(l.startswith("severity:") for l in existing):
        verdict.add_labels.append(severity)
    if is_bug and not (is_feature or is_compat) and "type:bug" not in existing:
        verdict.add_labels.append("type:bug")

    # --- 1. completeness ---------------------------------------------------
    missing: list[str] = []
    if parsed.freeform:
        missing.append(
            "the report was not filed through an issue form, so none of the "
            "environment details are present — please open a new issue using "
            "**Bug report** so the fields are captured"
        )
    else:
        for spec in parsed.missing_required():
            missing.append(f"**{spec.label}**")

        if is_bug:
            if parsed.has("steps") and not _looks_like_steps(parsed.get("steps")):
                missing.append(
                    "**Steps to reproduce** — what is there is a single line; "
                    "please give the numbered steps somebody else can follow"
                )
            usb_or_transfer = {"area:usb", "area:transfer", "area:saves"} & set(
                verdict.add_labels
            )
            if usb_or_transfer and not parsed.has("diagnostics"):
                missing.append(
                    "**Diagnostics report** — this one is about the connection or a "
                    "transfer, and without the doctor output there is nothing to go on "
                    "(**Switch ▸ Copy Diagnostics** in the app)"
                )
        if is_compat and parsed.get("outcome", "").casefold().startswith(
            "the console was never detected"
        ) and not parsed.has("diagnostics"):
            missing.append(
                "**Diagnostics report** — for a console that is never detected this is "
                "the only thing that shows what was on the USB bus"
            )

    # --- 2. known issues ---------------------------------------------------
    matches = [k for k in known_issues if k.matches(text, parsed)]
    verdict.matches = matches
    for known in matches:
        for label in known.labels:
            # `status:*` records a maintainer's decision. The bot may route and
            # grade an issue, but claiming a state a human never set would also
            # lock it out of its own hands-off list — and with it the promise to
            # reopen a disputed answer.
            if label.startswith("status:"):
                continue
            if label not in verdict.add_labels:
                verdict.add_labels.append(label)

    high = [k for k in matches if k.confidence == "high"]
    medium = [k for k in matches if k.confidence == "medium"]

    # --- 3. duplicates -----------------------------------------------------
    summary = parsed.get("what-happened") or parsed.get("problem") or parsed.get("details") or body
    duplicate_of, dup_score = find_duplicate(title, summary, open_issues)

    # --- 4. verdict --------------------------------------------------------
    if high:
        verdict.kind = "answered"
        # Closing is opt-in per known issue, never inferred. An entry that could
        # plausibly also describe a real defect answers and leaves the issue open.
        verdict.should_close = all(k.close for k in high) and not is_feature
        verdict.add_labels.append("triage:answered")
        verdict.reasons.append(
            f"Matched {len(high)} documented issue"
            f"{'s' if len(high) > 1 else ''}: "
            + ", ".join(k.title for k in high)
        )
    elif duplicate_of is not None:
        verdict.kind = "duplicate"
        verdict.duplicate_of = duplicate_of
        verdict.add_labels.append("duplicate")
        verdict.reasons.append(
            f"Overlaps #{duplicate_of} closely (similarity {dup_score:.0%})"
        )
    elif missing:
        verdict.kind = "needs-info"
        verdict.missing = missing
        verdict.add_labels.append("needs-info")
        verdict.reasons.append(
            f"{len(missing)} required detail{'s' if len(missing) > 1 else ''} "
            "missing from the report"
        )
        if medium:
            verdict.reasons.append(
                "Possible cause: " + ", ".join(k.title for k in medium)
            )
    else:
        actionable, why = _is_actionable(
            parsed=parsed,
            text=text,
            is_bug=is_bug,
            is_feature=is_feature,
            is_compat=is_compat,
            severity=severity,
            areas=[l for l in verdict.add_labels if l.startswith("area:")],
            current_version=current_version,
        )
        verdict.reasons.extend(why)
        if actionable:
            verdict.kind = "pr-recommended"
            verdict.add_labels.append("triage:pr-recommended")
        else:
            verdict.kind = "needs-human"
            verdict.add_labels.append("triage:needs-human")
        if medium:
            verdict.reasons.append(
                "Possible cause: " + ", ".join(k.title for k in medium)
            )

    # Clear stale triage state whenever the verdict moves on.
    for label in (
        "needs-info",
        "triage:pr-recommended",
        "triage:needs-human",
        "triage:answered",
        "duplicate",
    ):
        if label in existing and label not in verdict.add_labels:
            verdict.remove_labels.append(label)

    verdict.add_labels = [l for l in dict.fromkeys(verdict.add_labels) if l not in existing]
    return verdict


def _is_actionable(
    *,
    parsed: ParsedIssue,
    text: str,
    is_bug: bool,
    is_feature: bool,
    is_compat: bool,
    severity: str,
    areas: Sequence[str],
    current_version: str,
) -> tuple[bool, list[str]]:
    """Decide whether attempting a fix is the right next step.

    The bar is deliberately high. A wrong "yes" spends a maintainer's review
    time on a speculative pull request; a wrong "no" costs one comment.
    """
    reasons: list[str] = []

    if is_feature:
        # Features are a design conversation first. The exception is a small,
        # well-scoped ask in an area that is cheap to change.
        if "area:docs" in areas:
            reasons.append("Documentation change with a concrete proposal")
            return True, reasons
        reasons.append("Feature requests are a design decision, not a triage one")
        return False, reasons

    if is_compat:
        outcome = parsed.get("outcome", "").casefold()
        if outcome.startswith("everything worked"):
            reasons.append("A positive compatibility report — nothing to fix")
            return False, reasons
        reasons.append("Compatibility failures need hardware a maintainer may not have")
        return False, reasons

    if "area:docs" in areas:
        reasons.append("Documentation fix, and the report says what is wrong")
        return True, reasons

    if not is_bug:
        reasons.append("Not classified as a defect")
        return False, reasons

    if severity == "severity:low" and "area:app" in areas:
        reasons.append("Small, self-contained interface fix")
        return True, reasons

    frequency = parsed.get("reproducible", "").casefold()
    if frequency and not (
        frequency.startswith("every time") or frequency.startswith("often")
    ):
        reasons.append(
            f"Reported as happening {frequency!r} — too intermittent to attempt a "
            "fix from the description alone"
        )
        return False, reasons

    if not _looks_like_steps(parsed.get("steps", "")):
        reasons.append("No reproduction steps that could drive a change")
        return False, reasons

    if not areas:
        reasons.append("Could not work out which part of the code this concerns")
        return False, reasons

    current = _version_is_current(parsed.get("version", ""), current_version)
    if current is False:
        reasons.append(
            f"Reported against {parsed.get('version')}, which is older than "
            f"{current_version} — worth confirming on the current release first"
        )
        return False, reasons

    if parsed.get("dbi-mode", "").casefold().startswith("applet"):
        reasons.append(
            "DBI was in applet mode, which causes failures unrelated to SwitchMTP"
        )
        return False, reasons

    reasons.append(
        "Complete report, reproducible, and the affected area is identified"
    )
    return True, reasons
