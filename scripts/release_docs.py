#!/usr/bin/env python3
"""Generate version-matched manual and changelog release assets."""

from __future__ import annotations

import argparse
import re
import sys
from datetime import date
from pathlib import Path

from worklease.cli import _aggregate_help, _parser

_VERSION = re.compile(r"(?:v)?([0-9]+\.[0-9]+\.[0-9]+)")
_RELEASE_HEADING = re.compile(
    r"^## (?P<version>[0-9]+\.[0-9]+\.[0-9]+) - (?P<date>[0-9]{4}-[0-9]{2}-[0-9]{2})$",
    re.MULTILINE,
)


def normalize_version(value: str) -> str:
    """Return a bare semantic release version."""
    match = _VERSION.fullmatch(value)
    if match is None:
        raise ValueError(f"invalid release version: {value!r}")
    return match.group(1)


def extract_release_changelog(changelog: str, version: str) -> tuple[str, str]:
    """Return the release date and exact Markdown section for ``version``."""
    expected = normalize_version(version)
    matches = [
        match
        for match in _RELEASE_HEADING.finditer(changelog)
        if match.group("version") == expected
    ]
    if not matches:
        raise ValueError(f"CHANGELOG.md has no release entry for {expected}")
    if len(matches) != 1:
        raise ValueError(f"CHANGELOG.md has duplicate release entries for {expected}")

    match = matches[0]
    try:
        date.fromisoformat(match.group("date"))
    except ValueError as error:
        raise ValueError(
            f"CHANGELOG.md release {expected} has an invalid date"
        ) from error
    next_heading = re.search(r"^## ", changelog[match.end() :], re.MULTILINE)
    end = match.end() + next_heading.start() if next_heading else len(changelog)
    section = changelog[match.start() : end].strip()
    if not re.search(r"^### ", section, re.MULTILINE):
        raise ValueError(f"CHANGELOG.md release {expected} has no categorized changes")
    return match.group("date"), section + "\n"


def _roff_literal(text: str) -> str:
    lines: list[str] = []
    for line in text.splitlines():
        escaped = line.replace("\\", r"\e").replace("-", r"\-")
        if escaped.startswith((".", "'")):
            escaped = r"\&" + escaped
        lines.append(escaped)
    return "\n".join(lines)


def render_man_page(version: str, release_date: str, help_text: str) -> str:
    """Render the complete CLI help as a portable roff manual page."""
    normalized = normalize_version(version)
    try:
        date.fromisoformat(release_date)
    except ValueError as error:
        raise ValueError(f"invalid release date: {release_date!r}") from error
    return (
        f'.TH WORKLEASE 1 "{release_date}" "worklease {normalized}" "User Commands"\n'
        ".SH NAME\n"
        "worklease \\- provider-neutral same-host work leases\n"
        ".SH SYNOPSIS\n"
        ".B worklease\n"
        "[global options] COMMAND [command options]\n"
        ".SH DESCRIPTION\n"
        "Worklease coordinates expiring local work claims across agents and processes.\n"
        ".SH COMPLETE COMMAND REFERENCE\n"
        ".nf\n"
        f"{_roff_literal(help_text.rstrip())}\n"
        ".fi\n"
        ".SH SEE ALSO\n"
        "Project documentation: https://github.com/brettinternet/worklease\n"
    )


def write_release_docs(
    version: str, changelog_path: Path, output_directory: Path
) -> tuple[Path, Path]:
    """Generate release-specific man-page and changelog assets."""
    normalized = normalize_version(version)
    release_date, section = extract_release_changelog(
        changelog_path.read_text(encoding="utf-8"), normalized
    )
    output_directory.mkdir(parents=True, exist_ok=True)
    man_page = output_directory / "worklease.1"
    release_notes = output_directory / f"worklease-v{normalized}-changelog.md"
    man_page.write_text(
        render_man_page(normalized, release_date, _aggregate_help(_parser())),
        encoding="utf-8",
    )
    release_notes.write_text(section, encoding="utf-8")
    return man_page, release_notes


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="release version, with or without a v prefix")
    parser.add_argument("--changelog", type=Path, default=Path("CHANGELOG.md"))
    parser.add_argument("--output-directory", type=Path, default=Path("dist/release"))
    args = parser.parse_args(argv)
    try:
        paths = write_release_docs(args.version, args.changelog, args.output_directory)
    except (OSError, ValueError) as error:
        print(f"release documentation generation failed: {error}", file=sys.stderr)
        return 1
    for path in paths:
        print(path)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
