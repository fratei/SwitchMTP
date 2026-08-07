#!/usr/bin/env python3
"""Merge `scripts/translations.json` into the app's String Catalog.

Xcode extracts new source strings into `Localizable.xcstrings` during a build,
but it only ever fills in the source language. This script is the other half:
it takes the hand-written translations kept next to it and writes them into the
catalog as `translated` string units, leaving anything Xcode already knows
about untouched.

Run it after adding user-facing strings:

    xcodebuild ... -exportLocalizations   # or just build, to extract
    python3 scripts/apply-translations.py

It is idempotent, and it reports anything that is still missing so an
untranslated string cannot slip in unnoticed.
"""

from __future__ import annotations

import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
CATALOG = ROOT / "app" / "Resources" / "Localizable.xcstrings"
TRANSLATIONS = ROOT / "scripts" / "translations.json"

LANGUAGES = ["en", "es", "ja", "ru", "zh-Hans", "zh-Hant"]

# Xcode rewrites `%@ ... %@` as positional `%1$@ ... %2$@` when a string has
# more than one placeholder. Translations are written in the plain form for
# readability, so normalise both sides before comparing counts.
PLACEHOLDER = re.compile(r"%(?:(\d+)\$)?(@|lld|ld|d|f|lf)")


def placeholders(value: str) -> list[str]:
    return [m.group(2) for m in PLACEHOLDER.finditer(value)]


def main() -> int:
    catalog = json.loads(CATALOG.read_text())
    spec = json.loads(TRANSLATIONS.read_text())
    strings = catalog["strings"]

    do_not_translate = set(spec["doNotTranslate"])
    translations = spec["translations"]

    added = 0
    marked = 0
    problems: list[str] = []

    for key in do_not_translate:
        entry = strings.get(key)
        if entry is None:
            problems.append(f"doNotTranslate key not in catalog: {key!r}")
            continue
        if entry.get("shouldTranslate") is not False:
            entry["shouldTranslate"] = False
            entry.pop("localizations", None)
            marked += 1

    for key, langs in translations.items():
        entry = strings.get(key)
        if entry is None:
            problems.append(f"translation key not in catalog: {key!r}")
            continue

        source = entry.get("localizations", {}).get("en", {}).get("stringUnit", {}).get("value", key)
        want = placeholders(source)

        locs = entry.setdefault("localizations", {})
        # The English unit is whatever the compiler extracted; keep it, but make
        # sure it exists and is marked translated rather than `new`.
        locs.setdefault("en", {"stringUnit": {"state": "translated", "value": source}})
        locs["en"]["stringUnit"]["state"] = "translated"

        for lang in LANGUAGES:
            if lang == "en":
                continue
            value = langs.get(lang)
            if value is None:
                problems.append(f"missing {lang} for {key!r}")
                continue
            got = placeholders(value)
            if sorted(got) != sorted(want):
                problems.append(
                    f"placeholder mismatch for {key!r} [{lang}]: source {want} vs translation {got}"
                )
                continue
            locs[lang] = {"stringUnit": {"state": "translated", "value": value}}
            added += 1

    # Report anything the catalog still cannot render in every language.
    untranslated = []
    for key, entry in sorted(strings.items()):
        if entry.get("shouldTranslate") is False:
            continue
        locs = entry.get("localizations", {})
        missing = [
            lang
            for lang in LANGUAGES
            if locs.get(lang, {}).get("stringUnit", {}).get("state") != "translated"
        ]
        if missing:
            untranslated.append((key, missing))

    CATALOG.write_text(json.dumps(catalog, ensure_ascii=False, indent=2, sort_keys=True) + "\n")

    print(f"marked do-not-translate: {marked}")
    print(f"wrote translations:      {added}")
    print(f"total keys:              {len(strings)}")

    for p in problems:
        print(f"PROBLEM: {p}", file=sys.stderr)
    for key, missing in untranslated:
        print(f"UNTRANSLATED: {key!r} missing {missing}", file=sys.stderr)

    return 1 if problems or untranslated else 0


if __name__ == "__main__":
    raise SystemExit(main())
