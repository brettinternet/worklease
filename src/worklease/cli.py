"""Command-line interface for the worklease package."""

from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Sequence

from . import __version__
from .execution import execute
from .models import LeaseError, MutationRequest
from .replacement import replace_file
from .store import LeaseStore


def _common_claim_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--resource", required=True)
    parser.add_argument("--claim-id", required=True)
    parser.add_argument("--token", required=True)
    parser.add_argument("--revision", required=True, type=int)
    parser.add_argument("--operation-id", required=True)
    parser.add_argument("--ttl", default=900.0, type=float)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="worklease")
    parser.add_argument(
        "--format",
        choices=("json", "text"),
        default="json",
        help="output format (default: json)",
    )
    parser.add_argument(
        "--home",
        help="override WORKLEASE_HOME for this command",
    )
    parser.add_argument(
        "--version",
        action="store_true",
        help="print the packaged worklease version",
    )
    commands = parser.add_subparsers(dest="operation")

    execute_parser = commands.add_parser("exec", help="run one argv under a lease")
    _common_claim_arguments(execute_parser)
    execute_parser.add_argument("command", nargs=argparse.REMAINDER)

    replace_parser = commands.add_parser(
        "replace-file", help="atomically replace a file by expected hash"
    )
    _common_claim_arguments(replace_parser)
    replace_parser.add_argument("--path", required=True)
    replace_parser.add_argument("--expected-sha256", required=True)
    replace_parser.add_argument("--content-file", required=True)
    return parser


def _emit(payload: dict[str, object], output_format: str) -> None:
    if output_format == "text":
        if payload.get("operation") == "version" and payload.get("ok"):
            print(payload["version"])
            return
        print(json.dumps(payload, sort_keys=True, separators=(",", ":")))
        return
    print(json.dumps(payload, sort_keys=True, separators=(",", ":")))


def _envelope(operation: str, payload: dict[str, object]) -> dict[str, object]:
    return {"schemaVersion": 1, "operation": operation, **payload}


def main(argv: Sequence[str] | None = None) -> int:
    parser = _parser()
    args = parser.parse_args(argv)
    if args.version:
        _emit(
            _envelope(
                "version",
                {"ok": True, "version": __version__},
            ),
            args.format,
        )
        return 0
    if args.operation is None:
        parser.print_help(sys.stderr)
        return 64

    try:
        store = LeaseStore(args.home)
        request = MutationRequest(
            resource=args.resource,
            claim_id=args.claim_id,
            token=args.token,
            revision=args.revision,
            operation_id=args.operation_id,
            ttl=args.ttl,
        )
        if args.operation == "exec":
            command = list(args.command)
            if command and command[0] == "--":
                command = command[1:]
            payload, child_code = execute(store, request, command)
        else:
            payload = replace_file(
                store,
                request,
                args.path,
                args.expected_sha256,
                args.content_file,
            )
            child_code = 0
        output = _envelope(args.operation, payload)
        _emit(output, args.format)
        if args.operation == "exec":
            return child_code
        return 0
    except LeaseError as error:
        output = _envelope(
            args.operation,
            {"ok": False, **error.as_dict()},
        )
        _emit(output, args.format)
        return error.code


if __name__ == "__main__":
    raise SystemExit(main())
