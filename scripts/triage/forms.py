"""Reading and validating the GitHub issue forms.

The triage engine needs to turn a rendered issue body back into the fields the
reporter filled in. GitHub renders a submitted form as a sequence of

    ### <the field's label>

    <the value, or "_No response_">

blocks — note that it writes the *label*, not the field `id`. Rather than
hard-coding that mapping and letting it rot the moment somebody rewords a
label, we read the templates themselves and build the mapping at run time.

This module is deliberately free of any GitHub API knowledge so it can be
exercised in tests without a network.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable, Sequence

import yaml

NO_RESPONSE = "_No response_"

#: Element types GitHub accepts in an issue form.
VALID_TYPES = {"markdown", "textarea", "input", "dropdown", "checkboxes"}

#: Keys GitHub accepts at the top level of an issue form.
VALID_TOP_LEVEL = {
    "name",
    "description",
    "body",
    "title",
    "labels",
    "assignees",
    "projects",
    "type",
}

_HEADING = re.compile(r"^###[ \t]+(.+?)[ \t]*$", re.MULTILINE)
_CHECKED = re.compile(r"^[-*][ \t]+\[[xX]\][ \t]+(.*)$")
_UNCHECKED = re.compile(r"^[-*][ \t]+\[[ \t]?\][ \t]+(.*)$")
_FENCE = re.compile(r"^```[^\n]*\n(.*?)\n?```\s*$", re.DOTALL)


@dataclass(frozen=True)
class FormField:
    id: str
    type: str
    label: str
    required: bool
    options: tuple[str, ...] = ()


@dataclass(frozen=True)
class IssueForm:
    filename: str
    name: str
    labels: tuple[str, ...]
    fields: tuple[FormField, ...]

    def by_label(self) -> dict[str, FormField]:
        return {normalise_label(f.label): f for f in self.fields}

    def by_id(self) -> dict[str, FormField]:
        return {f.id: f for f in self.fields}


@dataclass
class ParsedIssue:
    """A rendered issue body decomposed back into form fields."""

    form: IssueForm | None
    values: dict[str, str] = field(default_factory=dict)
    checked: dict[str, list[str]] = field(default_factory=dict)
    unknown_headings: list[str] = field(default_factory=list)
    #: True when the body has no `###` headings at all, i.e. it was written by
    #: hand or through the API rather than submitted through a form.
    freeform: bool = False

    def get(self, field_id: str, default: str = "") -> str:
        return self.values.get(field_id, default)

    def has(self, field_id: str) -> bool:
        return bool(self.values.get(field_id, "").strip())

    def missing_required(self) -> list[FormField]:
        if self.form is None:
            return []
        out = []
        for f in self.form.fields:
            if not f.required or f.type == "checkboxes":
                continue
            if not self.has(f.id):
                out.append(f)
        return out


def normalise_label(text: str) -> str:
    """Labels survive a round trip through GitHub's renderer imperfectly.

    Folding whitespace and case, and dropping trailing punctuation, makes the
    lookup robust against the small cosmetic edits people make to templates.
    """
    text = re.sub(r"\s+", " ", (text or "").replace("\u00a0", " ")).strip()
    return text.rstrip(":?").strip().casefold()


def load_forms(template_dir: str | Path) -> list[IssueForm]:
    directory = Path(template_dir)
    forms: list[IssueForm] = []
    for path in sorted(directory.glob("*.y*ml")):
        if path.name == "config.yml":
            continue
        forms.append(load_form(path))
    return forms


def load_form(path: str | Path) -> IssueForm:
    path = Path(path)
    data = yaml.safe_load(path.read_text(encoding="utf-8"))
    fields: list[FormField] = []
    for element in data.get("body", []):
        if element.get("type") == "markdown":
            continue
        attrs = element.get("attributes") or {}
        options: tuple[str, ...] = ()
        raw_options = attrs.get("options") or []
        if element.get("type") == "dropdown":
            options = tuple(str(o) for o in raw_options)
        elif element.get("type") == "checkboxes":
            options = tuple(str(o.get("label", "")) for o in raw_options)
        fields.append(
            FormField(
                id=str(element.get("id", "")),
                type=str(element.get("type", "")),
                label=str(attrs.get("label", "")),
                required=bool((element.get("validations") or {}).get("required", False)),
                options=options,
            )
        )
    labels = data.get("labels") or []
    return IssueForm(
        filename=path.name,
        name=str(data.get("name", path.stem)),
        labels=tuple(str(x) for x in labels),
        fields=tuple(fields),
    )


def validate_forms(template_dir: str | Path) -> list[str]:
    """Return a list of human-readable problems; empty means the forms are good.

    GitHub's own error message when a form is malformed is famously terse and
    only appears once someone tries to open an issue, so we check the rules that
    actually bite: unknown keys, missing ids, duplicate ids, `validations`
    accidentally nested inside `attributes`, and dropdowns with no options.
    """
    directory = Path(template_dir)
    problems: list[str] = []
    referenced_labels: set[str] = set()

    config = directory / "config.yml"
    if config.exists():
        data = yaml.safe_load(config.read_text(encoding="utf-8")) or {}
        extra = set(data) - {"blank_issues_enabled", "contact_links"}
        if extra:
            problems.append(f"config.yml: unsupported keys {sorted(extra)}")
        for i, link in enumerate(data.get("contact_links") or []):
            missing = {"name", "url", "about"} - set(link)
            if missing:
                problems.append(f"config.yml: contact_links[{i}] missing {sorted(missing)}")

    for path in sorted(directory.glob("*.y*ml")):
        if path.name == "config.yml":
            continue
        try:
            data = yaml.safe_load(path.read_text(encoding="utf-8"))
        except yaml.YAMLError as exc:
            problems.append(f"{path.name}: not valid YAML: {exc}")
            continue
        if not isinstance(data, dict):
            problems.append(f"{path.name}: top level must be a mapping")
            continue

        extra = set(data) - VALID_TOP_LEVEL
        if extra:
            problems.append(f"{path.name}: unsupported top-level keys {sorted(extra)}")
        for key in ("name", "description"):
            if not str(data.get(key, "")).strip():
                problems.append(f"{path.name}: '{key}' is required and must not be empty")
        body = data.get("body")
        if not isinstance(body, list) or not body:
            problems.append(f"{path.name}: 'body' must be a non-empty list")
            continue

        seen_ids: set[str] = set()
        for i, element in enumerate(body):
            where = f"{path.name}: body[{i}]"
            if not isinstance(element, dict):
                problems.append(f"{where}: must be a mapping")
                continue
            etype = element.get("type")
            if etype not in VALID_TYPES:
                problems.append(f"{where}: unsupported type {etype!r}")
                continue
            attrs = element.get("attributes")
            if not isinstance(attrs, dict):
                problems.append(f"{where}: missing 'attributes'")
                continue
            if "validations" in attrs:
                problems.append(
                    f"{where}: 'validations' is nested inside 'attributes'; "
                    "it must be a sibling of it"
                )
            stray = set(element) - {"type", "id", "attributes", "validations"}
            if stray:
                problems.append(f"{where}: unsupported keys {sorted(stray)}")
            validations = element.get("validations") or {}
            if not isinstance(validations, dict) or (set(validations) - {"required"}):
                problems.append(f"{where}: 'validations' supports only 'required'")

            if etype == "markdown":
                if "id" in element:
                    problems.append(f"{where}: markdown blocks must not have an id")
                if not str(attrs.get("value", "")).strip():
                    problems.append(f"{where}: markdown block has no 'value'")
                if validations:
                    problems.append(f"{where}: markdown blocks cannot be required")
                continue

            element_id = element.get("id")
            if not element_id:
                problems.append(f"{where}: {etype} needs an id (triage keys off it)")
            elif element_id in seen_ids:
                problems.append(f"{where}: duplicate id {element_id!r}")
            else:
                seen_ids.add(str(element_id))

            if not str(attrs.get("label", "")).strip():
                problems.append(f"{where}: missing 'label'")

            if etype == "dropdown":
                options = attrs.get("options")
                if not options:
                    problems.append(f"{where}: dropdown has no options")
                else:
                    if len(options) != len(set(options)):
                        problems.append(f"{where}: dropdown has duplicate options")
                    if "default" in attrs:
                        index = attrs["default"]
                        if not isinstance(index, int) or not 0 <= index < len(options):
                            problems.append(f"{where}: 'default' is out of range")
            if etype == "checkboxes":
                options = attrs.get("options") or []
                if not options:
                    problems.append(f"{where}: checkboxes block has no options")
                for j, option in enumerate(options):
                    if not isinstance(option, dict) or not str(option.get("label", "")).strip():
                        problems.append(f"{where}: options[{j}] needs a 'label'")

        for wanted in data.get("labels") or []:
            referenced_labels.add(str(wanted))

    # Cross-check that every label referenced by a form exists in labels.yml,
    # because GitHub silently drops labels that have not been created yet.
    labels_file = directory.parent / "labels.yml"
    if referenced_labels and labels_file.exists():
        known = {
            str(entry.get("name"))
            for entry in (yaml.safe_load(labels_file.read_text(encoding="utf-8")) or [])
            if isinstance(entry, dict)
        }
        for name in sorted(referenced_labels - known):
            problems.append(
                f"labels.yml: issue forms apply the label {name!r}, which is not defined there"
            )

    return problems


def strip_fence(text: str) -> str:
    """Undo the ```-fencing GitHub adds around `render:`-ed textareas."""
    match = _FENCE.match(text.strip())
    return match.group(1).strip() if match else text.strip()


def split_sections(body: str) -> list[tuple[str, str]]:
    """Split a rendered issue body into (heading, value) pairs."""
    body = (body or "").replace("\r\n", "\n").replace("\r", "\n")
    matches = list(_HEADING.finditer(body))
    sections: list[tuple[str, str]] = []
    for i, match in enumerate(matches):
        start = match.end()
        end = matches[i + 1].start() if i + 1 < len(matches) else len(body)
        sections.append((match.group(1).strip(), body[start:end].strip()))
    return sections


def parse_issue(body: str, forms: Iterable[IssueForm]) -> ParsedIssue:
    """Decompose a rendered issue body using whichever form best explains it."""
    sections = split_sections(body or "")
    if not sections:
        return ParsedIssue(form=None, freeform=True)

    headings = [normalise_label(h) for h, _ in sections]

    best: IssueForm | None = None
    best_score = 0
    for form in forms:
        known = set(form.by_label())
        score = sum(1 for h in headings if h in known)
        if score > best_score:
            best, best_score = form, score

    parsed = ParsedIssue(form=best)
    lookup = best.by_label() if best else {}

    for heading, raw in sections:
        key = normalise_label(heading)
        spec = lookup.get(key)
        if spec is None:
            parsed.unknown_headings.append(heading)
            continue
        if spec.type == "checkboxes":
            ticked = [
                m.group(1).strip()
                for line in raw.split("\n")
                if (m := _CHECKED.match(line.strip()))
            ]
            parsed.checked[spec.id] = ticked
            # Record something in `values` too so `has()` behaves sensibly.
            parsed.values[spec.id] = "\n".join(ticked)
            continue
        value = raw.strip()
        if value == NO_RESPONSE:
            value = ""
        if spec.type == "textarea":
            value = strip_fence(value)
        parsed.values[spec.id] = value.strip()

    return parsed


_INLINE_FIELD = re.compile(r"^(?P<label>[^:\n]{3,120}?)[ \t]*[:：][ \t]*(?P<value>\S.*)$")
_LEADING_NOISE = re.compile(r"^[ \t>*\-+\d.)]+")


def _label_candidates(prefix: str) -> list[str]:
    """Every plausible label hiding at the end of the text before a colon.

    People write "Sorry — macOS version: 26.6", not a bare field label, so the
    match has to be against a trailing slice rather than the whole line.
    """
    cleaned = _LEADING_NOISE.sub("", prefix).strip().strip("*_`").strip()
    words = cleaned.split()
    if not words:
        return []
    return [" ".join(words[i:]) for i in range(max(0, len(words) - 6), len(words))]


def apply_followups(parsed: ParsedIssue, comments: Sequence[str]) -> ParsedIssue:
    """Fold details supplied in later comments into an incomplete report.

    Triage tells reporters they can fill gaps in a comment rather than opening a
    new issue, so it has to actually look there. Only fields that are still
    empty get filled: a comment can complete a report but never overwrite what
    the reporter originally wrote.

    Two shapes are understood — a repeat of the `### Label` heading, and the far
    more likely `Label: value` on a single line.
    """
    if parsed.form is None:
        return parsed

    lookup = parsed.form.by_label()

    for comment in comments:
        if not comment:
            continue

        for heading, raw in split_sections(comment):
            spec = lookup.get(normalise_label(heading))
            if spec is None or spec.type == "checkboxes" or parsed.has(spec.id):
                continue
            value = raw.strip()
            if value and value != NO_RESPONSE:
                parsed.values[spec.id] = (
                    strip_fence(value) if spec.type == "textarea" else value
                )

        for line in comment.split("\n"):
            match = _INLINE_FIELD.match(line.rstrip())
            if not match:
                continue
            for candidate in _label_candidates(match.group("label")):
                spec = lookup.get(normalise_label(candidate))
                if spec is None or spec.type == "checkboxes" or parsed.has(spec.id):
                    continue
                parsed.values[spec.id] = match.group("value").strip()
                break

    return parsed
