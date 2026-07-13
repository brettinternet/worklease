"""Command-line interface for the worklease package."""

from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Sequence
from typing import NoReturn

from . import __version__
from .adapters import key_result
from .execution import execute
from .models import AcquireRequest, LeaseError, MutationRequest
from .replacement import replace_file
from .store import LeaseStore

_COMMANDS = frozenset(
    {"key", "acquire", "status", "list", "heartbeat", "release", "exec", "replace-file"}
)


class _ArgumentError(Exception):
    """A parser failure that can be represented by the JSON CLI contract."""

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message


class _ArgumentParser(argparse.ArgumentParser):
    """ArgumentParser variant that leaves output formatting to :func:`main`."""

    def error(self, message: str) -> NoReturn:
        raise _ArgumentError(message)


def _add_output_arguments(
    parser: argparse.ArgumentParser, *, include_format: bool = True
) -> None:
    if include_format:
        parser.add_argument(
            "--format",
            choices=("json", "text"),
            default=argparse.SUPPRESS,
            help="output format (default: json)",
        )
    parser.add_argument(
        "--home",
        default=argparse.SUPPRESS,
        help="override WORKLEASE_HOME for this command",
    )


def _common_claim_arguments(
    parser: argparse.ArgumentParser, *, include_ttl: bool = True
) -> None:
    parser.add_argument("--resource", required=True)
    parser.add_argument("--claim-id", required=True)
    parser.add_argument("--token", required=True)
    parser.add_argument("--revision", required=True, type=int)
    parser.add_argument("--operation-id", required=True)
    if include_ttl:
        parser.add_argument("--ttl", default=900.0, type=float)


def _parser() -> _ArgumentParser:
    parser = _ArgumentParser(prog="worklease")
    parser.add_argument(
        "--format",
        choices=("json", "text"),
        default="json",
        help="output format (default: json)",
    )
    parser.add_argument(
        "--home",
        default=None,
        help="override WORKLEASE_HOME for this command",
    )
    parser.add_argument(
        "--version",
        action="store_true",
        help="print the packaged worklease version",
    )
    commands = parser.add_subparsers(dest="operation", parser_class=_ArgumentParser)

    key_parser = commands.add_parser("key", help="derive one stable resource key")
    _add_output_arguments(key_parser)
    key_parser.add_argument("--provider", required=True)
    key_parser.add_argument("--source", required=True)
    key_parser.add_argument("--item", required=True)
    key_parser.add_argument(
        "--coordination-only",
        action="store_true",
        help="derive a lease that coordinates local workers without fencing writes",
    )

    acquire_parser = commands.add_parser(
        "acquire", help="atomically acquire or reclaim a lease"
    )
    _add_output_arguments(acquire_parser)
    acquire_parser.add_argument("--resource", required=True)
    acquire_parser.add_argument("--claim-id", required=True)
    acquire_parser.add_argument("--agent-id", required=True)
    acquire_parser.add_argument("--session-id", required=True)
    acquire_parser.add_argument("--owner-id", required=True)
    acquire_parser.add_argument("--work-key", required=True)
    acquire_parser.add_argument(
        "--coordination-only",
        action="store_true",
        help="mark this ownership epoch as unable to fence provider writes",
    )
    acquire_parser.add_argument("--ttl", default=900.0, type=float)

    status_parser = commands.add_parser("status", help="read current lease state")
    _add_output_arguments(status_parser)
    status_parser.add_argument("--resource", required=True)

    list_parser = commands.add_parser("list", help="list current and expired claims")
    _add_output_arguments(list_parser)
    list_parser.add_argument("--resource", help="filter claims to one exact resource")

    heartbeat_parser = commands.add_parser("heartbeat", help="renew an active lease")
    _add_output_arguments(heartbeat_parser)
    _common_claim_arguments(heartbeat_parser)

    release_parser = commands.add_parser("release", help="release an active lease")
    _add_output_arguments(release_parser)
    _common_claim_arguments(release_parser, include_ttl=False)
    release_parser.add_argument("--reason", required=True)

    execute_parser = commands.add_parser("exec", help="run one argv under a lease")
    _add_output_arguments(execute_parser)
    _common_claim_arguments(execute_parser)
    execute_parser.add_argument("command", nargs=argparse.REMAINDER)

    replace_parser = commands.add_parser(
        "replace-file", help="atomically replace a file by expected hash"
    )
    _add_output_arguments(replace_parser)
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
        if payload.get("operation") == "list" and payload.get("ok"):
            print("STATE\tRESOURCE\tCLAIM_ID\tOWNER_ID\tEXPIRES_AT")
            claims = payload.get("claims", [])
            if isinstance(claims, list):
                for claim in claims:
                    if not isinstance(claim, dict):
                        continue
                    print(
                        "\t".join(
                            (
                                "active" if claim.get("active") else "expired",
                                str(claim.get("resource", "")),
                                str(claim.get("claimId", "")),
                                str(claim.get("ownerId", "")),
                                str(claim.get("expiresAt", "")),
                            )
                        )
                    )
            return
        print(json.dumps(payload, sort_keys=True, separators=(",", ":")))
        return
    print(json.dumps(payload, sort_keys=True, separators=(",", ":")))


def _envelope(operation: str, payload: dict[str, object]) -> dict[str, object]:
    return {"schemaVersion": 1, "operation": operation, **payload}


def _operation_hint(argv: Sequence[str]) -> str:
    for value in argv:
        if value in _COMMANDS:
            return value
    return "parse"


def _request(args: argparse.Namespace) -> MutationRequest:
    return MutationRequest(
        resource=args.resource,
        claim_id=args.claim_id,
        token=args.token,
        revision=args.revision,
        operation_id=args.operation_id,
        ttl=getattr(args, "ttl", 900.0),
    )


def _dispatch(
    args: argparse.Namespace, store: LeaseStore | None
) -> tuple[dict[str, object], int]:
    operation = args.operation
    if operation == "key":
        return (
            key_result(
                args.provider,
                args.source,
                args.item,
                coordination_only=args.coordination_only,
            ),
            0,
        )
    if operation == "acquire":
        assert store is not None
        return (
            store.acquire(
                AcquireRequest(
                    resource=args.resource,
                    claim_id=args.claim_id,
                    agent_id=args.agent_id,
                    session_id=args.session_id,
                    owner_id=args.owner_id,
                    work_key=args.work_key,
                    ttl=args.ttl,
                    coordination_only=args.coordination_only,
                )
            ),
            0,
        )
    if operation == "status":
        assert store is not None
        return store.status(args.resource), 0
    if operation == "list":
        assert store is not None
        return store.list_claims(args.resource), 0
    if operation == "heartbeat":
        assert store is not None
        return store.heartbeat(_request(args)), 0
    if operation == "release":
        assert store is not None
        return store.release(_request(args), args.reason), 0
    if operation == "exec":
        assert store is not None
        command = list(args.command)
        if command and command[0] == "--":
            command = command[1:]
        return execute(store, _request(args), command)
    if operation == "replace-file":
        assert store is not None
        return (
            replace_file(
                store,
                _request(args),
                args.path,
                args.expected_sha256,
                args.content_file,
            ),
            0,
        )
    raise _ArgumentError("missing-command")


def _fallback_output_format(argv: Sequence[str]) -> str:
    """Choose a safe format for parser errors without reading child argv."""

    options = list(argv)
    if "--" in options:
        options = options[: options.index("--")]
    for index, value in enumerate(options):
        if value == "--format" and index + 1 < len(options):
            return "text" if options[index + 1] == "text" else "json"
        if value == "--format=text":
            return "text"
    return "json"


def main(argv: Sequence[str] | None = None) -> int:
    values = list(sys.argv[1:] if argv is None else argv)
    output_format = _fallback_output_format(values)
    try:
        args = _parser().parse_args(values)
        output_format = args.format
    except _ArgumentError:
        _emit(
            _envelope(
                _operation_hint(values),
                {"ok": False, "error": "invalid-arguments"},
            ),
            output_format,
        )
        return 64

    if args.version:
        _emit(
            _envelope(
                "version",
                {"ok": True, "version": __version__},
            ),
            output_format,
        )
        return 0
    if args.operation is None:
        _emit(
            _envelope(
                "parse",
                {"ok": False, "error": "missing-command"},
            ),
            output_format,
        )
        return 64

    try:
        store = (
            None if args.operation == "key" else LeaseStore(getattr(args, "home", None))
        )
        payload, child_code = _dispatch(args, store)
        output = _envelope(args.operation, payload)
        _emit(output, output_format)
        return child_code
    except LeaseError as error:
        output = _envelope(
            args.operation,
            {"ok": False, **error.as_dict()},
        )
        _emit(output, output_format)
        return error.code


if __name__ == "__main__":
    raise SystemExit(main())
