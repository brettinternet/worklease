#!/usr/bin/env python3
"""Embed the validated published release version into package source."""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

_VERSION_PATTERN = re.compile(
    r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)"
    r"(?:[-+][0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
)
_DEFAULT_TARGET = Path("src/worklease/_release_metadata.py")


def render_release_metadata(version: str) -> str:
    """Return source for the package's published-version metadata module."""
    if _VERSION_PATTERN.fullmatch(version) is None:
        raise ValueError(f"invalid release version: {version!r}")
    return (
        '"""Build-time metadata for published documentation references."""\n\n'
        f"PUBLISHED_RELEASE_VERSION: str | None = {version!r}\n"
    )


def write_release_metadata(version: str, target: Path = _DEFAULT_TARGET) -> None:
    """Write validated published-version metadata to the package source."""
    target.write_text(render_release_metadata(version), encoding="utf-8")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "version", help="published release version without the v prefix"
    )
    parser.add_argument("--target", type=Path, default=_DEFAULT_TARGET)
    args = parser.parse_args(argv)
    try:
        write_release_metadata(args.version, args.target)
    except (OSError, ValueError) as error:
        print(f"release metadata generation failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
