#!/usr/bin/env python3
"""Generate version-matched manual and changelog release assets."""

from __future__ import annotations

import argparse
import re
import sys
import textwrap
from datetime import date
from pathlib import Path

from worklease.cli import _canonical_subparsers, _parser

_VERSION = re.compile(r"(?:v)?([0-9]+\.[0-9]+\.[0-9]+)")
_RELEASE_HEADING = re.compile(
    r"^## (?P<version>[0-9]+\.[0-9]+\.[0-9]+) - (?P<date>[0-9]{4}-[0-9]{2}-[0-9]{2})$",
    re.MULTILINE,
)
_COMMAND_GROUPS = (
    (
        "Single leases",
        "key, acquire, status, list, heartbeat, checkpoint, exec, release, transfer",
        "Derive a stable resource key, then manage one lease. Use exec to run a command only while the lease remains valid. Use transfer only when ownership must change.",
    ),
    (
        "Bundles",
        "acquire-bundle, status-bundle, heartbeat-bundle, exec-bundle, release-bundle",
        "Manage several resources as one atomic lease. Every bundle command uses the same ordered resource set.",
    ),
    (
        "Inspection and recovery",
        "history, inspect-operation, inspect-operation-bundle, reconcile-operation, reconcile-operation-bundle",
        "Read retained events and resolve an operation whose outcome is unknown. Inspect before reconciling.",
    ),
    (
        "Maintenance",
        "policy, gc, replace-file, instructions",
        "Inspect key policies, collect old records, replace a file by expected hash, or print concise coordination guidance.",
    ),
)
MAN_PAGE_EXAMPLES = (
    (
        "1. Basic lease",
        '''WORKLEASE_AGENT_ID=manual worklease acquire --resource build --lease-file ./build.lease
WORKLEASE_AGENT_ID=manual worklease status --resource build
WORKLEASE_AGENT_ID=manual worklease release --lease-file ./build.lease --reason "build finished"''',
    ),
    (
        "2. Private lease-file lifecycle",
        '''WORKLEASE_AGENT_ID=manual lease_dir="$(mktemp -d)"
WORKLEASE_AGENT_ID=manual lease_file="$lease_dir/deploy.lease"
trap 'WORKLEASE_AGENT_ID=manual worklease release --lease-file "$lease_file" --reason "shell exited" >/dev/null 2>&1 || true' EXIT
WORKLEASE_AGENT_ID=manual worklease acquire --resource deploy --lease-file "$lease_file"
WORKLEASE_AGENT_ID=manual worklease heartbeat --lease-file "$lease_file"
WORKLEASE_AGENT_ID=manual worklease release --lease-file "$lease_file" --reason "deploy finished"''',
    ),
    (
        "3. Guarded command",
        '''WORKLEASE_AGENT_ID=manual lease_dir="$(mktemp -d)"
WORKLEASE_AGENT_ID=manual lease_file="$lease_dir/guarded.lease"
trap 'rc=$?; if [ "$rc" -ne 0 ]; then worklease release --lease-file "$lease_file" --reason "guarded command failed" >/dev/null 2>&1 || true; fi; exit "$rc"' EXIT
WORKLEASE_AGENT_ID=manual worklease acquire --resource guarded --lease-file "$lease_file"
WORKLEASE_AGENT_ID=manual worklease exec --lease-file "$lease_file" -- /bin/echo guarded
WORKLEASE_AGENT_ID=manual worklease release --lease-file "$lease_file" --reason "guarded command finished"''',
    ),
    (
        "4. Atomic bundle",
        '''WORKLEASE_AGENT_ID=manual lease_dir="$(mktemp -d)"
WORKLEASE_AGENT_ID=manual lease_file="$lease_dir/bundle.lease"
trap 'rc=$?; if [ "$rc" -ne 0 ]; then worklease release-bundle --lease-file "$lease_file" --reason "bundle command failed" >/dev/null 2>&1 || true; fi; exit "$rc"' EXIT
WORKLEASE_AGENT_ID=manual worklease acquire-bundle --resource api --resource worker --lease-file "$lease_file"
WORKLEASE_AGENT_ID=manual worklease exec-bundle --resource api --resource worker --lease-file "$lease_file" -- /bin/echo guarded
WORKLEASE_AGENT_ID=manual worklease release-bundle --lease-file "$lease_file" --reason "bundle command finished"''',
    ),
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


def _roff_paragraph(text: str) -> str:
    return textwrap.fill(text, width=76, break_long_words=False, break_on_hyphens=False)


def _current_commands() -> set[str]:
    parser = _parser()
    for action in parser._actions:
        if isinstance(action, argparse._SubParsersAction):
            return {name for name, _, _ in _canonical_subparsers(action)}
    raise ValueError("worklease parser has no commands")


def _documented_commands() -> set[str]:
    return {
        command
        for _, commands, _ in _COMMAND_GROUPS
        for command in commands.split(", ")
    }


def render_man_page(version: str, release_date: str) -> str:
    """Render a concise, task-oriented roff manual page."""
    normalized = normalize_version(version)
    try:
        date.fromisoformat(release_date)
    except ValueError as error:
        raise ValueError(f"invalid release date: {release_date!r}") from error
    current = _current_commands()
    documented = _documented_commands()
    if current != documented:
        raise ValueError(
            "manual command inventory mismatch: "
            f"missing={sorted(current - documented)}, "
            f"extra={sorted(documented - current)}"
        )

    sections = [
        f'.TH WORKLEASE 1 "{release_date}" "worklease {normalized}" "User Commands"',
        ".SH NAME",
        "worklease \\- same-host expiring work leases",
        ".SH SYNOPSIS",
        ".B worklease",
        "[global options] COMMAND [command options]",
        ".SH DESCRIPTION",
        _roff_paragraph(
            "Worklease manages provider-neutral, expiring leases on one host. Prefer lease files for normal workflows. Tokens are bearer credentials; never log or expose them. JSON output must be explicitly requested with either \\fB--json\\fR or \\fB--format json\\fR."
        ),
        ".SH CORE WORKFLOW",
        _roff_paragraph(
            "Create or select a key, acquire a lease, inspect or heartbeat it while work continues, then release it. Use checkpoints for progress, \\fBexec\\fR for guarded commands, and bundle commands for atomic multi-lease workflows. Use transfer only when ownership must change."
        ),
        ".SH COMMANDS",
    ]
    for heading, commands, description in _COMMAND_GROUPS:
        sections.extend(
            (
                f'.SS "{heading}"',
                ".B " + _roff_literal(commands),
                _roff_paragraph(_roff_literal(description)),
            )
        )
    sections.extend(
        (
            ".PP",
            _roff_paragraph(
                "Run \\fBworklease COMMAND --help\\fR for complete options. Run \\fBworklease --help-all\\fR for every command."
            ),
            ".SH SAFETY",
            _roff_paragraph(
                "Lease files are preferred because they keep lifecycle state in a controlled file. Treat tokens as bearer credentials: protect them, do not log them, and avoid placing them in command history or process arguments when possible. Expiration does not stop a process; guarded commands must handle lease loss. Use bundle commands when several leases must change together."
            ),
            ".SH GLOBAL OPTIONS",
            ".TP",
            ".B --json",
            "Return the schema-versioned JSON result. Text output is the default.",
            ".TP",
            ".B --home DIRECTORY",
            "Use DIRECTORY for Worklease state.",
            ".TP",
            ".B --version",
            "Print the packaged version.",
            ".SH FILES AND ENVIRONMENT",
            _roff_paragraph(
                "State location precedence is \\fB--home\\fR, \\fBWORKLEASE_HOME\\fR, \\fBXDG_STATE_HOME/worklease\\fR, then \\fB~/.local/state/worklease\\fR."
            ),
            ".PP",
            "Use a private lease file for sensitive lifecycle state.",
            ".SH EXAMPLES",
        )
    )
    for heading, example in MAN_PAGE_EXAMPLES:
        sections.extend(
            (
                f'.SS "{heading}"',
                ".nf",
                _roff_literal(example),
                ".fi",
            )
        )
    sections.extend(
        (
            ".SH SEE ALSO",
            "\\fBworklease instructions\\fR, \\fBworklease policy\\fR, \\fBworklease history\\fR",
            ".PP",
            "Project documentation: https://github.com/brettinternet/worklease",
        )
    )
    return "\n".join(sections) + "\n"


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
        render_man_page(normalized, release_date),
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
