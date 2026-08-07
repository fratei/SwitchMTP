"""Tests for the triage engine.

    python3 -m pytest scripts/triage/test_triage.py -q

The bodies below are written the way GitHub renders a submitted issue form:
`### <label>` followed by the value, with `_No response_` for the fields the
reporter skipped. Getting that shape right is most of what the parser has to
cope with, so the fixtures are built from the real templates rather than
hand-written, and a drift in either direction fails the tests.
"""

from __future__ import annotations

import sys
from pathlib import Path

import pytest
import yaml

sys.path.insert(0, str(Path(__file__).resolve().parent))

import report
import triage
from forms import (
    apply_followups,
    load_forms,
    normalise_label,
    parse_issue,
    split_sections,
    validate_forms,
)
from rules import IssueRef, evaluate, find_duplicate, load_known_issues, similarity

ROOT = Path(__file__).resolve().parents[2]
TEMPLATES = ROOT / ".github" / "ISSUE_TEMPLATE"
KNOWN = Path(__file__).resolve().parent / "known_issues.yml"

FORMS = load_forms(TEMPLATES)
KNOWN_ISSUES = load_known_issues(yaml.safe_load(KNOWN.read_text(encoding="utf-8")))

BUG = "1-bug-report.yml"
FEATURE = "2-feature-request.yml"
COMPAT = "3-compatibility-report.yml"


# ------------------------------------------------------------------ fixtures

def form(filename: str):
    for f in FORMS:
        if f.filename == filename:
            return f
    raise AssertionError(f"no such form: {filename}")


def render_body(filename: str, values: dict[str, object]) -> str:
    """Build an issue body exactly as GitHub would render the submitted form."""
    spec = form(filename)
    unknown = set(values) - {f.id for f in spec.fields}
    assert not unknown, f"{filename} has no fields {sorted(unknown)}"

    chunks: list[str] = []
    for f in spec.fields:
        if f.id not in values:
            if f.type == "checkboxes":
                lines = [f"- [ ] {o}" for o in f.options]
                chunks.append(f"### {f.label}\n\n" + "\n".join(lines))
            else:
                chunks.append(f"### {f.label}\n\n_No response_")
            continue
        value = values[f.id]
        if f.type == "checkboxes":
            ticked = set(value or ())
            lines = [
                f"- [{'X' if o in ticked else ' '}] {o}" for o in f.options
            ]
            chunks.append(f"### {f.label}\n\n" + "\n".join(lines))
        elif f.type == "dropdown" and isinstance(value, (list, tuple)):
            chunks.append(f"### {f.label}\n\n" + "\n".join(str(v) for v in value))
        elif f.type == "textarea" and str(value).startswith("```"):
            chunks.append(f"### {f.label}\n\n{value}")
        else:
            chunks.append(f"### {f.label}\n\n{value}")
    return "\n\n".join(chunks) + "\n"


def complete_bug(**overrides: object) -> dict[str, object]:
    values: dict[str, object] = {
        "what-happened": (
            "Selecting three files on the SD card and exporting them writes only "
            "the first one. The other two never appear and no error is shown."
        ),
        "steps": (
            "1. Launch DBI in title mode and start the MTP responder\n"
            "2. Open SwitchMTP and select SD Card\n"
            "3. Navigate to /switch and select three .nro files\n"
            "4. Press Export and choose ~/Desktop\n"
            "5. Only the first file is written"
        ),
        "reproducible": "Every time",
        "area": ["Downloading from the console to the Mac"],
        "version": "1.0.0 (1)",
        "install-source": "A release download from GitHub",
        "macos": "26.6",
        "hardware": "Apple Silicon",
        "hos": "22.5.0",
        "dbi": "v658",
        "dbi-mode": "Title mode (held R while starting a game)",
        "connection": "Cable straight from the console to the Mac",
        "diagnostics": '{"devices": [{"vid": 1406, "pid": 8221}]}',
        "checks": [
            "I read the Troubleshooting guide and my problem is not covered there.",
            "I searched existing issues and this has not already been reported.",
        ],
    }
    values.update(overrides)
    return values


def judge(body: str, *, title: str, labels=("type:bug",), others=(), version="1.0.0"):
    parsed = parse_issue(body, FORMS)
    verdict = evaluate(
        title=title,
        body=body,
        parsed=parsed,
        existing_labels=list(labels),
        known_issues=KNOWN_ISSUES,
        open_issues=list(others),
        current_version=version,
    )
    return parsed, verdict


# -------------------------------------------------------------- the templates

def test_templates_are_valid():
    assert validate_forms(TEMPLATES) == []


def test_every_form_has_the_ids_triage_depends_on():
    required = {
        BUG: {"what-happened", "steps", "reproducible", "area", "version",
              "macos", "hardware", "dbi-mode", "diagnostics", "logs"},
        FEATURE: {"problem", "proposal", "area"},
        COMPAT: {"outcome", "version", "macos", "details", "diagnostics"},
    }
    for filename, ids in required.items():
        have = {f.id for f in form(filename).fields}
        assert ids <= have, f"{filename} lost {sorted(ids - have)}"


def test_known_issue_docs_exist():
    for known in KNOWN_ISSUES:
        if not known.doc:
            continue
        path = ROOT / known.doc.split("#", 1)[0]
        assert path.exists(), f"{known.id} points at a missing file: {known.doc}"


def test_known_issue_ids_are_unique():
    ids = [k.id for k in KNOWN_ISSUES]
    assert len(ids) == len(set(ids))


def test_known_issue_labels_exist():
    defined = {
        entry["name"]
        for entry in yaml.safe_load(
            (ROOT / ".github" / "labels.yml").read_text(encoding="utf-8")
        )
    }
    for known in KNOWN_ISSUES:
        for label in known.labels:
            assert label in defined, f"{known.id} uses undefined label {label!r}"


def test_triage_labels_are_defined():
    defined = {
        entry["name"]
        for entry in yaml.safe_load(
            (ROOT / ".github" / "labels.yml").read_text(encoding="utf-8")
        )
    }
    used = {
        "type:bug", "needs-info", "duplicate", "triage:answered",
        "triage:pr-recommended", "triage:needs-human", "triage:delegated",
        "severity:data-loss", "severity:crash", "severity:high",
        "severity:medium", "severity:low",
        "area:usb", "area:transfer", "area:backend", "area:app",
        "area:cli", "area:saves", "area:build", "area:docs",
    }
    assert used <= defined, sorted(used - defined)


# ------------------------------------------------------------------- parsing

def test_split_sections_handles_crlf_and_blank_values():
    body = "### One\r\n\r\nfirst\r\n\r\n### Two\r\n\r\n_No response_\r\n"
    assert split_sections(body) == [("One", "first"), ("Two", "_No response_")]


def test_parse_maps_labels_back_to_ids():
    parsed = parse_issue(render_body(BUG, complete_bug()), FORMS)
    assert parsed.form is not None and parsed.form.filename == BUG
    assert parsed.get("version") == "1.0.0 (1)"
    assert parsed.get("hardware") == "Apple Silicon"
    assert parsed.unknown_headings == []
    assert not parsed.freeform


def test_no_response_becomes_empty():
    body = render_body(BUG, {k: v for k, v in complete_bug().items() if k != "logs"})
    parsed = parse_issue(body, FORMS)
    assert parsed.get("logs") == ""
    assert not parsed.has("logs")


def test_rendered_code_fences_are_stripped():
    body = render_body(BUG, complete_bug(logs="```shell\nopen failed\n```"))
    parsed = parse_issue(body, FORMS)
    assert parsed.get("logs") == "open failed"


def test_checkboxes_record_only_the_ticked_ones():
    parsed = parse_issue(render_body(BUG, complete_bug()), FORMS)
    assert len(parsed.checked["checks"]) == 2
    assert all("latest" not in c for c in parsed.checked["checks"])


def test_reworded_label_still_matches():
    body = render_body(BUG, complete_bug()).replace(
        "### macOS version", "### macOS  Version:"
    )
    parsed = parse_issue(body, FORMS)
    assert parsed.get("macos") == "26.6"


def test_freeform_body_is_detected():
    parsed = parse_issue("it just doesn't work, please fix", FORMS)
    assert parsed.freeform and parsed.form is None


def test_the_right_form_is_chosen():
    for filename, values in (
        (FEATURE, {"problem": "x", "proposal": "y"}),
        (COMPAT, {"outcome": "Everything worked", "details": "all good"}),
    ):
        parsed = parse_issue(render_body(filename, values), FORMS)
        assert parsed.form is not None and parsed.form.filename == filename


def test_normalise_label_folds_punctuation_and_case():
    assert normalise_label("  macOS  Version? ") == normalise_label("macos version")


# ------------------------------------------------------------- completeness

def test_missing_required_fields_produce_needs_info():
    body = render_body(BUG, {k: v for k, v in complete_bug().items()
                             if k not in ("version", "macos")})
    _, verdict = judge(body, title="Export writes only the first file")
    assert verdict.kind == "needs-info"
    assert any("SwitchMTP version" in m for m in verdict.missing)
    assert any("macOS version" in m for m in verdict.missing)
    assert "needs-info" in verdict.add_labels


def test_one_line_steps_are_rejected():
    body = render_body(BUG, complete_bug(steps="it breaks"))
    _, verdict = judge(body, title="Export writes only the first file")
    assert verdict.kind == "needs-info"
    assert any("Steps to reproduce" in m for m in verdict.missing)


def test_transfer_bug_without_diagnostics_asks_for_them():
    body = render_body(BUG, {k: v for k, v in complete_bug().items()
                             if k != "diagnostics"})
    _, verdict = judge(body, title="Export writes only the first file")
    assert verdict.kind == "needs-info"
    assert any("Diagnostics" in m for m in verdict.missing)


def test_interface_bug_does_not_demand_diagnostics():
    body = render_body(
        BUG,
        {k: v for k, v in complete_bug(
            **{
                "area": ["The window, menus or general interface"],
                "what-happened": "The sidebar header is clipped at narrow widths.",
                "steps": "1. Open the app\n2. Drag the window narrower than 700pt\n"
                         "3. The header text is cut off",
            }
        ).items() if k != "diagnostics"},
    )
    _, verdict = judge(body, title="Sidebar header is clipped")
    assert verdict.kind != "needs-info", verdict.missing


def test_freeform_issue_is_asked_to_use_the_form():
    _, verdict = judge("nothing works", title="broken", labels=())
    assert verdict.kind == "needs-info"
    assert any("issue form" in m for m in verdict.missing)


# ------------------------------------------------------------- known issues

def test_icloud_downloads_is_answered_and_closed():
    body = render_body(
        BUG,
        complete_bug(
            **{
                "what-happened": "I exported a folder but the files are not in "
                                 "my Downloads folder afterwards.",
                "area": ["Downloading from the console to the Mac"],
            }
        ),
    )
    _, verdict = judge(body, title="Exported files are missing")
    assert verdict.kind == "answered"
    assert verdict.should_close
    assert "triage:answered" in verdict.add_labels
    assert any(k.id == "icloud-downloads" for k in verdict.matches)


def test_extra_buffers_is_answered():
    body = render_body(
        BUG,
        complete_bug(
            **{"what-happened": "The Switch shows 'Extra buffers exceeded. Media "
                                "write speed is too low' and the install aborts."}
        ),
    )
    _, verdict = judge(body, title="Install fails on the console")
    assert verdict.kind == "answered"
    assert any(k.id == "applet-mode-buffers" for k in verdict.matches)


def test_wrong_usb_mode_is_answered():
    body = render_body(
        BUG,
        complete_bug(
            **{
                "what-happened": "The console shows up as 0x3000 and SwitchMTP "
                                 "never lists it.",
                "area": ["Finding or connecting to the console"],
            }
        ),
    )
    _, verdict = judge(body, title="Console not detected")
    assert verdict.kind == "answered"
    assert any(k.id == "wrong-usb-mode" for k in verdict.matches)


def test_gatekeeper_is_answered():
    body = render_body(
        BUG,
        complete_bug(
            **{
                "what-happened": 'macOS says SwitchMTP "is damaged and can\'t be '
                                 'opened" when I double click it.',
                "area": ["The window, menus or general interface"],
            }
        ),
    )
    _, verdict = judge(body, title="App will not open")
    assert verdict.kind == "answered"
    assert any(k.id == "gatekeeper" for k in verdict.matches)


def test_applet_mode_is_only_a_hint_not_an_answer():
    """A medium-confidence match must not close anybody's issue."""
    body = render_body(
        BUG,
        {k: v for k, v in complete_bug(
            **{
                "what-happened": "The upload stalls at 40% and then fails.",
                "dbi-mode": "Applet mode (opened from the album)",
            }
        ).items() if k != "diagnostics"},
    )
    _, verdict = judge(body, title="Upload stalls")
    assert verdict.kind == "needs-info"
    assert not verdict.should_close
    assert any("applet" in r.casefold() for r in verdict.reasons)


def test_a_normal_bug_matches_nothing():
    _, verdict = judge(
        render_body(BUG, complete_bug()),
        title="Export writes only the first of several selected files",
    )
    assert [k.id for k in verdict.matches] == []


# The rules below exist because each of these phrasings used to trip a
# high-confidence rule and auto-close a real defect. They are the guard rail.

@pytest.mark.parametrize(
    "text,title,must_not_match",
    [
        (
            "Copying a 4 GB video to the SD card fails at 90% with a write error.",
            "Large copy fails near the end",
            "size-unknown",
        ),
        (
            "Uploading to Install to SD starts the install but the console reboots.",
            "Console reboots during install",
            "install-storage-not-browsable",
        ),
        (
            "I used to use Awoo Installer. With SwitchMTP the file list is empty.",
            "File list is empty",
            "wrong-usb-mode",
        ),
        (
            "Deleting a file on the SD card reports success but the file is still "
            "there after a refresh, so the SD card behaves read-only.",
            "Delete does not take effect",
            "readonly-storage",
        ),
        (
            "The transfer failed and afterwards the exported files are missing "
            "from the destination folder.",
            "Transfer failed and left nothing behind",
            "icloud-downloads",
        ),
    ],
)
def test_broad_phrasings_do_not_trip_a_canned_answer(text, title, must_not_match):
    body = render_body(BUG, complete_bug(**{"what-happened": text}))
    _, verdict = judge(body, title=title)
    assert must_not_match not in [k.id for k in verdict.matches], verdict.reasons


def test_only_entries_marked_close_can_close_an_issue():
    closable = {k.id for k in KNOWN_ISSUES if k.close}
    assert closable == {
        "icloud-downloads",
        "applet-mode-buffers",
        "no-dates",
        "gatekeeper",
    }, "closing behaviour changed — is the new entry really unambiguous?"


def test_an_answer_that_may_also_be_a_real_bug_stays_open():
    body = render_body(
        BUG,
        complete_bug(
            **{
                "what-happened": "SwitchMTP reports LIBUSB_ERROR_BUSY and never "
                                 "connects to the console.",
                "area": ["Finding or connecting to the console"],
            }
        ),
    )
    _, verdict = judge(body, title="Cannot connect")
    assert verdict.kind == "answered"
    assert not verdict.should_close
    assert any(k.id == "image-capture-claiming" for k in verdict.matches)


# --------------------------------------------------------------- duplicates

def test_duplicate_detection_finds_the_obvious_case():
    others = [
        IssueRef(
            number=7,
            title="Exporting several selected files only writes the first one",
            body="Selecting multiple files and exporting writes only one file.",
        )
    ]
    _, verdict = judge(
        render_body(BUG, complete_bug()),
        title="Export writes only the first of several selected files",
        others=others,
    )
    assert verdict.kind == "duplicate"
    assert verdict.duplicate_of == 7


def test_unrelated_issues_are_not_duplicates():
    others = [
        IssueRef(number=3, title="Add a dark mode toggle to the toolbar",
                 body="I would like a manual dark mode switch.")
    ]
    _, verdict = judge(
        render_body(BUG, complete_bug()),
        title="Export writes only the first of several selected files",
        others=others,
    )
    assert verdict.kind != "duplicate"


def test_similarity_is_symmetric_and_bounded():
    a, b = "usb device not detected", "device not detected over usb"
    assert similarity(a, b) == similarity(b, a)
    assert 0.0 <= similarity(a, b) <= 1.0
    assert similarity("", "anything") == 0.0


def test_find_duplicate_respects_the_threshold():
    others = [IssueRef(number=1, title="totally unrelated wording here", body="")]
    number, score = find_duplicate("export writes one file", "", others)
    assert number is None and score < 0.55


# ------------------------------------------------------------------ routing

@pytest.mark.parametrize(
    "option,expected",
    [
        ("Finding or connecting to the console", "area:usb"),
        ("Uploading from the Mac to the console", "area:transfer"),
        ("Backing up or restoring saves", "area:saves"),
        ("The command line tool", "area:cli"),
        ("Renaming, deleting or creating folders", "area:backend"),
        ("Building from source", "area:build"),
    ],
)
def test_area_dropdown_routes(option, expected):
    body = render_body(BUG, complete_bug(area=[option]))
    _, verdict = judge(body, title="Something is wrong")
    assert expected in verdict.add_labels


def test_keyword_routing_covers_freeform_reports():
    _, verdict = judge(
        "libusb_claim_interface fails and the device never enumerates",
        title="Cannot claim the USB interface",
        labels=(),
    )
    assert "area:usb" in verdict.add_labels


# ----------------------------------------------------------------- severity

@pytest.mark.parametrize(
    "text,expected",
    [
        ("The app deleted my saves from the SD card", "severity:data-loss"),
        ("SwitchMTP crashes as soon as I open the SD card", "severity:crash"),
        ("There is a typo in the export button label", "severity:low"),
        ("Transfers are completely broken and never work", "severity:high"),
    ],
)
def test_severity_classification(text, expected):
    body = render_body(BUG, complete_bug(**{"what-happened": text}))
    _, verdict = judge(body, title=text)
    assert expected in verdict.add_labels


def test_severity_is_not_reapplied_when_already_set():
    body = render_body(BUG, complete_bug())
    _, verdict = judge(body, title="x", labels=("type:bug", "severity:low"))
    assert not any(l.startswith("severity:") for l in verdict.add_labels)


# ------------------------------------------------------------ actionability

def test_a_complete_reproducible_bug_is_pr_recommended():
    _, verdict = judge(
        render_body(BUG, complete_bug()),
        title="Export writes only the first of several selected files",
    )
    assert verdict.kind == "pr-recommended"
    assert "triage:pr-recommended" in verdict.add_labels


def test_an_intermittent_bug_is_not():
    body = render_body(BUG, complete_bug(reproducible="Rarely"))
    _, verdict = judge(body, title="Export sometimes writes only one file")
    assert verdict.kind == "needs-human"
    assert "triage:needs-human" in verdict.add_labels


def test_an_old_version_is_asked_to_retest():
    body = render_body(BUG, complete_bug(version="0.9.0 (1)"))
    _, verdict = judge(
        body, title="Export writes only the first file", version="1.2.0"
    )
    assert verdict.kind == "needs-human"
    assert any("older" in r for r in verdict.reasons)


def test_applet_mode_blocks_a_fix_attempt():
    body = render_body(
        BUG,
        complete_bug(**{"dbi-mode": "Applet mode (opened from the album)",
                        "what-happened": "Copying a large folder to the SD card "
                                         "ends early without an error."}),
    )
    _, verdict = judge(body, title="Large copy ends early")
    assert verdict.kind == "needs-human"
    assert any("applet mode" in r for r in verdict.reasons)


def test_feature_requests_are_left_for_a_human():
    body = render_body(
        FEATURE,
        {
            "problem": "I sync two consoles and reselect the same folder each time.",
            "proposal": "Remember the last folder per console.",
            "area": ["Browsing files and folders"],
        },
    )
    _, verdict = judge(body, title="Remember the last folder", labels=("type:feature",))
    assert verdict.kind == "needs-human"


def test_a_positive_compatibility_report_is_not_a_fix_request():
    body = render_body(
        COMPAT,
        {
            "outcome": "Everything worked",
            "responder": "DBI v658",
            "console": "Switch OLED, HOS 21.0.0",
            "version": "1.0.0",
            "macos": "26.5, Intel",
            "connection": "Official dock",
            "details": "Browsed, downloaded and uploaded without trouble.",
        },
    )
    _, verdict = judge(body, title="Works on an Intel Mac", labels=("type:compatibility",))
    assert verdict.kind == "needs-human"
    assert any("positive" in r.casefold() for r in verdict.reasons)


def test_a_docs_bug_is_worth_a_pr():
    body = render_body(
        BUG,
        complete_bug(
            **{
                "what-happened": "The README documentation says to press Y in DBI "
                                 "but the correct key is X.",
                "area": [],
                "steps": "1. Open the README\n2. Read the DBI setup section\n"
                         "3. It names the wrong button",
            }
        ),
    )
    _, verdict = judge(body, title="README names the wrong DBI button")
    assert "area:docs" in verdict.add_labels
    assert verdict.kind == "pr-recommended"


# ------------------------------------------------------- follow-up comments

def test_a_followup_comment_completes_the_report():
    body = render_body(BUG, {k: v for k, v in complete_bug().items()
                             if k not in ("macos", "version")})
    parsed = parse_issue(body, FORMS)
    assert not parsed.has("macos")

    apply_followups(parsed, ["Sorry — macOS version: 26.6\nSwitchMTP version: 1.0.0 (1)"])
    assert parsed.get("macos") == "26.6"
    assert parsed.get("version") == "1.0.0 (1)"

    verdict = evaluate(
        title="Export writes only the first file",
        body=body,
        parsed=parsed,
        existing_labels=["type:bug", "needs-info"],
        known_issues=KNOWN_ISSUES,
        current_version="1.0.0",
    )
    assert verdict.kind == "pr-recommended"
    assert "needs-info" in verdict.remove_labels


def test_a_followup_using_the_heading_form_also_works():
    body = render_body(BUG, {k: v for k, v in complete_bug().items() if k != "macos"})
    parsed = parse_issue(body, FORMS)
    apply_followups(parsed, ["### macOS version\n\n26.4\n"])
    assert parsed.get("macos") == "26.4"


def test_a_followup_never_overwrites_the_original_answer():
    parsed = parse_issue(render_body(BUG, complete_bug()), FORMS)
    apply_followups(parsed, ["macOS version: 15.0"])
    assert parsed.get("macos") == "26.6"


def test_the_bots_own_comment_cannot_satisfy_the_request():
    """The needs-info list quotes every field label back at the reporter."""
    body = render_body(BUG, {k: v for k, v in complete_bug().items() if k != "macos"})
    parsed, verdict = judge(body, title="Incomplete report")
    bot_comment = report.render(verdict, parsed)

    author = "reporter"
    comments = [
        {"body": bot_comment, "user": {"login": "github-actions[bot]", "type": "Bot"}},
        {"body": "any news?", "user": {"login": author, "type": "User"}},
    ]
    kept = triage.followup_bodies(comments, author)
    assert bot_comment not in kept
    assert kept == ["any news?"]

    fresh = parse_issue(body, FORMS)
    apply_followups(fresh, kept)
    assert not fresh.has("macos")


def test_only_the_reporters_comments_are_read():
    comments = [
        {"body": "macOS version: 26.6", "user": {"login": "someone-else", "type": "User"}},
        {"body": "steps: 1. open\n2. click", "user": {"login": "reporter", "type": "User"}},
    ]
    assert triage.followup_bodies(comments, "reporter") == ["steps: 1. open\n2. click"]


def test_followups_are_ignored_for_freeform_issues():
    parsed = parse_issue("no form used", FORMS)
    apply_followups(parsed, ["macOS version: 26.6"])
    assert parsed.values == {}


# ----------------------------------------------------------- label hygiene

def test_stale_triage_labels_are_removed_when_the_verdict_changes():
    _, verdict = judge(
        render_body(BUG, complete_bug()),
        title="Export writes only the first file",
        labels=("type:bug", "needs-info"),
    )
    assert verdict.kind == "pr-recommended"
    assert "needs-info" in verdict.remove_labels


def test_labels_already_present_are_not_re_added():
    _, verdict = judge(
        render_body(BUG, complete_bug()),
        title="Export writes only the first file",
        labels=("type:bug", "area:transfer"),
    )
    assert "area:transfer" not in verdict.add_labels
    assert "type:bug" not in verdict.add_labels


# ------------------------------------------------------------------- report

def test_every_verdict_renders_with_the_marker():
    from rules import MARKER

    for values, title, labels in (
        (complete_bug(), "Export writes only the first file", ("type:bug",)),
        (complete_bug(**{"what-happened": "files are not in my downloads folder"}),
         "Missing files", ("type:bug",)),
        ({k: v for k, v in complete_bug().items() if k != "version"},
         "Incomplete", ("type:bug",)),
    ):
        parsed, verdict = judge(render_body(BUG, values), title=title, labels=labels)
        text = report.render(verdict, parsed)
        assert text.startswith(MARKER)
        assert "How this was classified" in text
        assert "rule-based script" in text
        assert len(text) < 8000


def test_needs_info_lists_what_is_missing():
    body = render_body(BUG, {k: v for k, v in complete_bug().items() if k != "macos"})
    parsed, verdict = judge(body, title="Incomplete report")
    text = report.render(verdict, parsed)
    assert "macOS version" in text
    assert "no need to open a new issue" in text


def test_answered_comment_carries_the_answer_and_the_doc_link():
    body = render_body(
        BUG,
        complete_bug(**{"what-happened": "exported files are not in my downloads"}),
    )
    parsed, verdict = judge(body, title="Missing files")
    text = report.render(verdict, parsed)
    assert "com~apple~CloudDocs" in text
    assert "docs/TROUBLESHOOTING.md" in text


def test_dry_run_is_visible_in_the_comment():
    parsed, verdict = judge(render_body(BUG, complete_bug()), title="x")
    assert "dry run" in report.render(verdict, parsed, dry_run=True)


# ------------------------------------------------------------- idempotence

def test_evaluating_twice_gives_the_same_answer():
    body = render_body(BUG, complete_bug())
    first = judge(body, title="Export writes only the first file")[1]
    second = judge(
        body,
        title="Export writes only the first file",
        labels=("type:bug", *first.add_labels),
    )[1]
    assert second.kind == first.kind
    assert second.add_labels == []
    assert second.remove_labels == []
