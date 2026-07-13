"""Command-line interface for the worklease package."""

from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Sequence

from . import __version__


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="worklease")
    parser.add_argument(
        "--format",
        choices=("json", "text"),
        default="json",
        help="output format (default: json)",
    )
    parser.add_argument(
        "--version",
        action="store_true",
        help="print the packaged worklease version",
    )
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = _parser().parse_args(argv)
    if not args.version:
        _parser().print_help(sys.stderr)
        return 64

    if args.format == "text":
        print(__version__)
        return 0

    print(
        json.dumps(
            {
                "schemaVersion": 1,
                "operation": "version",
                "ok": True,
                "version": __version__,
            },
            separators=(",", ":"),
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
