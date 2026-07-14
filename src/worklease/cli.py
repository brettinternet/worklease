"""Command-line interface for the worklease package."""

from __future__ import annotations

import argparse
import json
import sys
from collections.abc import Sequence
from typing import Any, NoReturn

from . import __version__
from .adapters import (
    describe_policy,
    key_result,
    policy_descriptors,
)
from .credentials import resolve_credential
from .execution import execute, execute_bundle
from .models import (
    AcquireRequest,
    BundleAcquireRequest,
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
    TransferRequest,
)
from .replacement import replace_file
from .store import LeaseStore

_COMMANDS = frozenset(
    {
        "key",
        "policy",
        "acquire",
        "acquire-bundle",
        "bundle-acquire",
        "status",
        "status-bundle",
        "bundle-status",
        "inspect-bundle",
        "inspect-operation",
        "reconcile-operation",
        "gc",
        "list",
        "heartbeat",
        "heartbeat-bundle",
        "bundle-heartbeat",
        "checkpoint",
        "transfer",
        "release",
        "release-bundle",
        "bundle-release",
        "exec",
        "exec-bundle",
        "bundle-exec",
        "replace-file",
    }
)


class _ArgumentError(Exception):
    """A parser failure that can be represented by the JSON CLI contract."""

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message


class _ArgumentParser(argparse.ArgumentParser):
    """ArgumentParser variant that leaves output formatting to :func:`main`."""

    def __init__(self, *args: Any, **kwargs: Any) -> None:
        kwargs.setdefault("allow_abbrev", False)
        super().__init__(*args, **kwargs)

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
    parser.add_argument("--token")
    parser.add_argument("--token-file")
    parser.add_argument("--token-fd")
    parser.add_argument("--revision", required=True, type=int)
    parser.add_argument("--operation-id", required=True)
    if include_ttl:
        parser.add_argument("--ttl", default=900.0, type=float)


def _execution_directory_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "--provider-directory",
        help="run provider commands from this validated working directory",
    )
    parser.add_argument(
        "--git-primary",
        action="store_true",
        help="derive the registered Git primary/control worktree",
    )


def _bundle_resources(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "--resource",
        "--resources",
        dest="resources",
        action="append",
        required=True,
        help="one exact opaque bundle resource (repeat for each member)",
    )


def _common_bundle_claim_arguments(
    parser: argparse.ArgumentParser, *, include_ttl: bool = True
) -> None:
    _bundle_resources(parser)
    parser.add_argument("--claim-id", required=True)
    parser.add_argument("--token")
    parser.add_argument("--token-file")
    parser.add_argument("--token-fd")
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

    policy_parser = commands.add_parser(
        "policy", help="inspect available resource-key policies"
    )
    policy_commands = policy_parser.add_subparsers(
        dest="policy_operation", required=True, parser_class=_ArgumentParser
    )
    list_policy_parser = policy_commands.add_parser(
        "list", help="list available resource-key policies"
    )
    _add_output_arguments(list_policy_parser)
    describe_parser = policy_commands.add_parser(
        "describe", help="describe one resource-key policy"
    )
    _add_output_arguments(describe_parser)
    describe_parser.add_argument("--name", required=True)

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

    acquire_bundle_parser = commands.add_parser(
        "acquire-bundle",
        aliases=("bundle-acquire",),
        help="atomically acquire or reclaim an opaque resource bundle",
    )
    _add_output_arguments(acquire_bundle_parser)
    _bundle_resources(acquire_bundle_parser)
    acquire_bundle_parser.add_argument("--claim-id", required=True)
    acquire_bundle_parser.add_argument("--agent-id", required=True)
    acquire_bundle_parser.add_argument("--session-id", required=True)
    acquire_bundle_parser.add_argument("--owner-id", required=True)
    acquire_bundle_parser.add_argument("--work-key", required=True)
    acquire_bundle_parser.add_argument("--coordination-only", action="store_true")
    acquire_bundle_parser.add_argument("--ttl", default=900.0, type=float)

    status_bundle_parser = commands.add_parser(
        "status-bundle",
        aliases=("bundle-status", "inspect-bundle"),
        help="inspect an exact resource bundle",
    )
    _add_output_arguments(status_bundle_parser)
    _bundle_resources(status_bundle_parser)

    status_parser = commands.add_parser("status", help="read current lease state")
    _add_output_arguments(status_parser)
    status_parser.add_argument("--resource", required=True)
    status_parser.add_argument(
        "--verbose",
        action="store_true",
        help="include redacted diagnostic metadata and unknown outcomes",
    )

    inspect_operation_parser = commands.add_parser(
        "inspect-operation", help="inspect one operation outcome"
    )
    _add_output_arguments(inspect_operation_parser)
    inspect_operation_parser.add_argument("--resource", required=True)
    inspect_operation_parser.add_argument("--operation-id", required=True)
    gc_parser = commands.add_parser(
        "gc", help="inspect or collect records eligible for garbage collection"
    )
    _add_output_arguments(gc_parser)
    gc_parser.add_argument(
        "--retention-days",
        default=argparse.SUPPRESS,
        type=float,
        help="retain records newer than this many days (default: 30)",
    )
    gc_parser.add_argument(
        "--cutoff",
        help="explicit UTC cutoff timestamp (ISO-8601)",
    )
    gc_parser.add_argument(
        "--apply",
        action="store_true",
        help="atomically delete records eligible under the selected cutoff",
    )

    reconcile_operation_parser = commands.add_parser(
        "reconcile-operation", help="record an observed operation outcome"
    )
    _add_output_arguments(reconcile_operation_parser)
    _common_claim_arguments(reconcile_operation_parser)
    reconcile_operation_parser.add_argument("--target-operation-id", required=True)
    reconcile_operation_parser.add_argument("--expected-request-sha256", required=True)
    reconcile_operation_parser.add_argument(
        "--outcome",
        required=True,
        choices=("observed-success", "observed-failure"),
    )
    reconcile_operation_parser.add_argument("--evidence", required=True)

    checkpoint_parser = commands.add_parser(
        "checkpoint", help="persist a bounded JSON checkpoint and renew a lease"
    )
    _add_output_arguments(checkpoint_parser)
    _common_claim_arguments(checkpoint_parser)
    checkpoint_parser.add_argument("--checkpoint", required=True)

    transfer_parser = commands.add_parser(
        "transfer", help="atomically transfer an active lease to a successor"
    )
    _add_output_arguments(transfer_parser)
    transfer_parser.add_argument("--resource", required=True)
    transfer_parser.add_argument("--claim-id", required=True)
    transfer_parser.add_argument("--token")
    transfer_parser.add_argument("--token-file")
    transfer_parser.add_argument("--token-fd")
    transfer_parser.add_argument("--revision", required=True, type=int)
    transfer_parser.add_argument("--operation-id", required=True)
    transfer_parser.add_argument("--successor-claim-id", required=True)
    transfer_parser.add_argument("--successor-agent-id", required=True)
    transfer_parser.add_argument("--successor-session-id", required=True)
    transfer_parser.add_argument("--successor-owner-id", required=True)
    transfer_parser.add_argument("--successor-work-key", required=True)
    transfer_parser.add_argument("--ttl", default=900.0, type=float)

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
    _execution_directory_arguments(execute_parser)
    execute_parser.add_argument("command", nargs=argparse.REMAINDER)

    heartbeat_bundle_parser = commands.add_parser(
        "heartbeat-bundle",
        aliases=("bundle-heartbeat",),
        help="renew every member of an active bundle",
    )
    _add_output_arguments(heartbeat_bundle_parser)
    _common_bundle_claim_arguments(heartbeat_bundle_parser)

    release_bundle_parser = commands.add_parser(
        "release-bundle",
        aliases=("bundle-release",),
        help="release every member of an active bundle",
    )
    _add_output_arguments(release_bundle_parser)
    _common_bundle_claim_arguments(release_bundle_parser, include_ttl=False)
    release_bundle_parser.add_argument("--reason", required=True)

    execute_bundle_parser = commands.add_parser(
        "exec-bundle",
        aliases=("bundle-exec",),
        help="run one argv under an opaque bundle claim",
    )
    _add_output_arguments(execute_bundle_parser)
    _common_bundle_claim_arguments(execute_bundle_parser)
    _execution_directory_arguments(execute_bundle_parser)
    execute_bundle_parser.add_argument("command", nargs=argparse.REMAINDER)
    replace_parser = commands.add_parser(
        "replace-file", help="atomically replace a file by expected hash"
    )
    _add_output_arguments(replace_parser)
    _common_claim_arguments(replace_parser)
    replace_parser.add_argument("--path", required=True)
    replace_parser.add_argument("--expected-sha256", required=True)
    replace_parser.add_argument("--content-file", required=True)
    return parser


def _text_value(value: object) -> str:
    """Render one text-mode scalar without allowing control-character injection."""

    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def _text_atom(value: object) -> str:
    """Keep simple labels readable while escaping control characters."""

    text = "" if value is None else str(value)
    if any(character in text for character in "\r\n\t"):
        return _text_value(text)
    return text


def _text_label(field: str) -> str:
    """Convert protocol camelCase names to stable readable labels."""

    result: list[str] = []
    for index, character in enumerate(field):
        if (
            character.isupper()
            and index
            and (field[index - 1].islower() or field[index - 1].isdigit())
        ):
            result.append("_")
        result.append(character.upper())
    return "".join(result)


def _text_header(payload: dict[str, object]) -> None:
    operation = str(payload.get("operation", "unknown"))
    if payload.get("ok"):
        print(f"OK {operation}")
    else:
        print(f"ERROR {operation}: {_text_atom(payload.get('error', 'unknown-error'))}")


def _emit_error_details(payload: dict[str, object]) -> None:
    """Print only stable, non-secret error details."""

    allowed = (
        "resource",
        "operationId",
        "targetOperationId",
        "provider",
        "field",
        "claimId",
        "expectedRevision",
        "suppliedRevision",
        "minimumExclusive",
        "maximumInclusive",
        "state",
        "guarantee",
        "providerFencing",
        "expectedRequestSha256",
    )
    for field in allowed:
        if field in payload:
            print(f"{_text_label(field)}\t{_text_value(payload[field])}")


def _claim_fields(
    claim: dict[str, object], *, include_token: bool = False
) -> tuple[tuple[str, object], ...]:
    fields: list[tuple[str, object]] = []
    if "resource" in claim:
        fields.append(("RESOURCE", claim["resource"]))
    if "resources" in claim:
        fields.append(("RESOURCES", claim["resources"]))
    fields.extend(
        (
            ("CLAIM_ID", claim.get("claimId")),
            ("REVISION", claim.get("revision")),
            ("EXPIRES_AT", claim.get("expiresAt")),
            ("GUARANTEE", claim.get("guarantee")),
        )
    )
    if include_token:
        fields.insert(2, ("TOKEN", claim.get("token")))
    return tuple(fields)


def _emit_claim(
    claim: object, *, include_token: bool = False, label: str = "CLAIM"
) -> None:
    if not isinstance(claim, dict):
        print(f"{label}\t<none>")
        return
    print(label)
    for field, value in _claim_fields(claim, include_token=include_token):
        print(f"{field}\t{_text_value(value)}")


def _emit_command(command: object) -> None:
    if not isinstance(command, dict):
        print("COMMAND\t<none>")
        return
    print("COMMAND")
    for field in ("returncode", "executionDirectory"):
        if field in command:
            print(f"{_text_label(field)}\t{_text_value(command[field])}")
    for field in ("stdout", "stderr"):
        if field in command:
            print(f"{_text_label(field)}\t{_text_value(command[field])}")


def _emit_verbose_status(payload: dict[str, object]) -> None:
    print(f"RESOURCE\t{_text_value(payload.get('resource', ''))}")
    print(f"STATE\t{payload.get('state', '')}")

    claim = payload.get("claim")
    if isinstance(claim, dict):
        print("CLAIM")
        for field in (
            "resource",
            "claimId",
            "agentId",
            "sessionId",
            "ownerId",
            "workKey",
            "coordinationOnly",
            "revision",
            "acquiredAt",
            "heartbeatAt",
            "expiresAt",
        ):
            print(f"{field}\t{_text_value(claim.get(field))}")
    else:
        print("CLAIM\t<none>")

    unknown_operations = payload.get("unknownOperations", [])
    if isinstance(unknown_operations, list):
        print(f"UNKNOWN_OPERATIONS\t{len(unknown_operations)}")
        for operation in unknown_operations:
            if not isinstance(operation, dict):
                continue
            print(
                "UNKNOWN\t"
                + "\t".join(
                    _text_value(operation.get(field))
                    for field in (
                        "operationId",
                        "kind",
                        "expectedRevision",
                        "createdAt",
                    )
                )
            )

    release = payload.get("release")
    if isinstance(release, dict):
        print("RELEASE")
        for field in ("claimId", "operationId", "revision", "releasedAt"):
            print(f"{field}\t{_text_value(release.get(field))}")
    else:
        print("RELEASE\t<none>")

    guidance = payload.get("guidance")
    if guidance is not None:
        print(f"GUIDANCE\t{_text_value(guidance)}")


def _render_version(payload: dict[str, object]) -> None:
    if payload.get("ok"):
        print(_text_atom(payload.get("version", "")))
    else:
        _text_header(payload)
        _emit_error_details(payload)


def _render_key(payload: dict[str, object]) -> None:
    _text_header(payload)
    for field in (
        "provider",
        "resource",
        "scope",
        "capability",
        "genericExecutionGuarantee",
        "fencedMutations",
        "providerFencing",
    ):
        if field in payload:
            print(f"{_text_label(field)}\t{_text_value(payload[field])}")
    if not payload.get("ok"):
        _emit_error_details(payload)


def _render_policy_list(payload: dict[str, object]) -> None:
    if not payload.get("ok"):
        _text_header(payload)
        _emit_error_details(payload)
        return
    print(
        "NAME\tORIGIN\tORIGIN_VERSION\tCONTRACT_VERSION\t"
        "KEY_POLICY_VERSION\tSCOPE\tCAPABILITY\t"
        "GENERIC_EXECUTION_GUARANTEE\tPROVIDER_FENCING_SUPPORTED"
    )
    policies = payload.get("policies", [])
    if isinstance(policies, list):
        fields = (
            "name",
            "origin",
            "originVersion",
            "contractVersion",
            "keyPolicyVersion",
            "scope",
            "capability",
            "genericExecutionGuarantee",
            "providerFencingSupported",
        )
        for policy in policies:
            if isinstance(policy, dict):
                print("\t".join(_text_atom(policy.get(field, "")) for field in fields))


def _render_policy_describe(payload: dict[str, object]) -> None:
    if not payload.get("ok"):
        _text_header(payload)
        _emit_error_details(payload)
        return
    fields = (
        "name",
        "origin",
        "originVersion",
        "contractVersion",
        "keyPolicyVersion",
        "scope",
        "capability",
        "providerFencingSupported",
    )
    for field in fields:
        if field in payload:
            print(f"{field}: {_text_atom(payload[field])}")


def _render_list(payload: dict[str, object]) -> None:
    if not payload.get("ok"):
        _text_header(payload)
        _emit_error_details(payload)
        return
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
                        _text_atom(claim.get("resource", "")),
                        _text_atom(claim.get("claimId", "")),
                        _text_atom(claim.get("ownerId", "")),
                        _text_atom(claim.get("expiresAt", "")),
                    )
                )
            )


def _render_status(payload: dict[str, object]) -> None:
    if not payload.get("ok"):
        _text_header(payload)
        _emit_error_details(payload)
        return
    _text_header(payload)
    if not isinstance(payload.get("claim"), dict):
        if "resource" in payload:
            print(f"RESOURCE\t{_text_value(payload['resource'])}")
        if "resources" in payload:
            print(f"RESOURCES\t{_text_value(payload['resources'])}")
    print(f"STATE\t{_text_atom(payload.get('state', ''))}")
    _emit_claim(payload.get("claim"))


def _render_status_verbose(payload: dict[str, object]) -> None:
    if payload.get("ok"):
        _emit_verbose_status(payload)
    else:
        _text_header(payload)
        _emit_error_details(payload)


def _render_inspect_operation(payload: dict[str, object]) -> None:
    if not payload.get("ok"):
        _text_header(payload)
        _emit_error_details(payload)
        return
    _text_header(payload)
    for field in (
        "resource",
        "operationId",
        "kind",
        "state",
        "outcome",
        "expectedRevision",
        "requestSha256",
        "reconciliationOperationId",
        "createdAt",
        "reconciledAt",
    ):
        if field in payload:
            print(f"{_text_label(field)}\t{_text_value(payload[field])}")


def _render_gc(payload: dict[str, object]) -> None:
    if not payload.get("ok"):
        _text_header(payload)
        _emit_error_details(payload)
        return
    _text_header(payload)
    for field in ("dryRun", "capturedAt", "cutoff", "retentionDays"):
        if field in payload:
            print(f"{_text_label(field)}\t{_text_value(payload[field])}")
    eligible = payload.get("eligible", {})
    if isinstance(eligible, dict):
        print("ELIGIBLE")
        for record_type in sorted(eligible):
            summary = eligible[record_type]
            if isinstance(summary, dict):
                print(
                    f"{record_type}\t{summary.get('count', 0)}\t"
                    f"{_text_value(summary.get('oldest'))}\t"
                    f"{_text_value(summary.get('newest'))}"
                )


def _render_mutation(payload: dict[str, object]) -> None:
    _text_header(payload)
    if not payload.get("ok"):
        _emit_error_details(payload)
        if "command" in payload:
            _emit_command(payload["command"])
        return
    for field in (
        "operationId",
        "targetOperationId",
        "releasedClaimId",
        "releasedRevision",
    ):
        if field in payload:
            print(f"{_text_label(field)}\t{_text_value(payload[field])}")
    if "checkpoint" in payload:
        print(f"CHECKPOINT\t{_text_value(payload['checkpoint'])}")
    if "checkpointBytes" in payload:
        print(f"CHECKPOINT_BYTES\t{_text_value(payload['checkpointBytes'])}")
    if "releasedAt" in payload:
        print(f"RELEASED_AT\t{_text_value(payload['releasedAt'])}")
    if "reason" in payload:
        print(f"REASON\t{_text_value(payload['reason'])}")
    if "ttl" in payload:
        print(f"TTL\t{_text_value(payload['ttl'])}")
    if "guarantee" in payload:
        print(f"GUARANTEE\t{_text_value(payload['guarantee'])}")
    if "providerFencing" in payload:
        print(f"PROVIDER_FENCING\t{_text_value(payload['providerFencing'])}")
    if "command" in payload:
        _emit_command(payload["command"])
    claim_token = payload.get("operation") in {
        "acquire",
        "acquire-bundle",
        "bundle-acquire",
        "heartbeat",
        "checkpoint",
        "heartbeat-bundle",
        "bundle-heartbeat",
        "transfer",
    }
    _emit_claim(payload.get("claim"), include_token=claim_token)


def _render_generic(payload: dict[str, object]) -> None:
    _text_header(payload)
    if not payload.get("ok"):
        _emit_error_details(payload)
        return
    for field in sorted(payload):
        if field in {"schemaVersion", "operation", "ok", "claim", "command"}:
            continue
        value = payload[field]
        if field == "token":
            continue
        print(f"{_text_label(field)}\t{_text_value(value)}")
    if "claim" in payload:
        _emit_claim(payload["claim"])
    if "command" in payload:
        _emit_command(payload["command"])


_TEXT_RENDERERS = {
    "version": _render_version,
    "key": _render_key,
    "policy-list": _render_policy_list,
    "policy-describe": _render_policy_describe,
    "list": _render_list,
    "status": _render_status,
    "status-bundle": _render_status,
    "bundle-status": _render_status,
    "inspect-bundle": _render_status,
    "status-verbose": _render_status_verbose,
    "inspect-operation": _render_inspect_operation,
    "gc": _render_gc,
    "acquire": _render_mutation,
    "acquire-bundle": _render_mutation,
    "bundle-acquire": _render_mutation,
    "heartbeat": _render_mutation,
    "checkpoint": _render_mutation,
    "heartbeat-bundle": _render_mutation,
    "bundle-heartbeat": _render_mutation,
    "transfer": _render_mutation,
    "release": _render_mutation,
    "release-bundle": _render_mutation,
    "bundle-release": _render_mutation,
    "exec": _render_mutation,
    "exec-bundle": _render_mutation,
    "bundle-exec": _render_mutation,
    "replace-file": _render_mutation,
    "reconcile-operation": _render_mutation,
}


def _emit(payload: dict[str, object], output_format: str) -> None:
    if output_format == "text":
        renderer = _TEXT_RENDERERS.get(
            str(payload.get("operation", "unknown")), _render_generic
        )
        renderer(payload)
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
        provider_directory=getattr(args, "provider_directory", None),
        git_primary=getattr(args, "git_primary", False),
    )


def _bundle_request(args: argparse.Namespace) -> BundleMutationRequest:
    return BundleMutationRequest(
        resources=tuple(args.resources),
        claim_id=args.claim_id,
        token=args.token,
        revision=args.revision,
        operation_id=args.operation_id,
        ttl=getattr(args, "ttl", 900.0),
        provider_directory=getattr(args, "provider_directory", None),
        git_primary=getattr(args, "git_primary", False),
    )


def _dispatch(
    args: argparse.Namespace, store: LeaseStore | None
) -> tuple[dict[str, object], int]:
    operation = args.operation
    if operation == "policy-list":
        return {
            "ok": True,
            "policies": [descriptor.to_dict() for descriptor in policy_descriptors()],
        }, 0
    if operation == "policy-describe":
        return {"ok": True, **describe_policy(args.name).to_dict()}, 0
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
    if operation in {"acquire-bundle", "bundle-acquire"}:
        assert store is not None
        return (
            store.acquire_bundle(
                BundleAcquireRequest(
                    resources=tuple(args.resources),
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
    if operation in {"status-bundle", "bundle-status", "inspect-bundle"}:
        assert store is not None
        return store.bundle_status(tuple(args.resources)), 0
    if operation == "status":
        assert store is not None
        return (
            store.status_verbose(args.resource)
            if args.verbose
            else store.status(args.resource),
            0,
        )
    if operation == "inspect-operation":
        assert store is not None
        return store.inspect_operation(args.resource, args.operation_id), 0
    if operation == "gc":
        assert store is not None
        return (
            store.garbage_collect(
                retention_days=getattr(args, "retention_days", None),
                cutoff=getattr(args, "cutoff", None),
                apply=getattr(args, "apply", False),
            ),
            0,
        )
    if operation == "reconcile-operation":
        assert store is not None
        try:
            evidence = json.loads(args.evidence)
        except (TypeError, ValueError) as error:
            raise LeaseError("invalid-evidence", code=64) from error
        return (
            store.reconcile_operation(
                _request(args),
                args.target_operation_id,
                args.expected_request_sha256,
                args.outcome,
                evidence,
            ),
            0,
        )
    if operation == "list":
        assert store is not None
        return store.list_claims(args.resource), 0
    if operation == "heartbeat":
        assert store is not None
        return store.heartbeat(_request(args)), 0
    if operation == "checkpoint":
        assert store is not None
        try:
            checkpoint = json.loads(args.checkpoint)
        except (TypeError, ValueError) as error:
            raise LeaseError("invalid-checkpoint", code=64) from error
        return store.checkpoint(_request(args), checkpoint), 0
    if operation == "transfer":
        assert store is not None
        return (
            store.transfer(
                TransferRequest(
                    resource=args.resource,
                    claim_id=args.claim_id,
                    token=args.token,
                    revision=args.revision,
                    operation_id=args.operation_id,
                    successor_claim_id=args.successor_claim_id,
                    successor_agent_id=args.successor_agent_id,
                    successor_session_id=args.successor_session_id,
                    successor_owner_id=args.successor_owner_id,
                    successor_work_key=args.successor_work_key,
                    ttl=args.ttl,
                )
            ),
            0,
        )
    if operation in {"heartbeat-bundle", "bundle-heartbeat"}:
        assert store is not None
        return store.heartbeat_bundle(_bundle_request(args)), 0
    if operation in {"release-bundle", "bundle-release"}:
        assert store is not None
        return store.release_bundle(_bundle_request(args), args.reason), 0
    if operation == "release":
        assert store is not None
        return store.release(_request(args), args.reason), 0
    if operation in {"exec-bundle", "bundle-exec"}:
        assert store is not None
        command = list(args.command)
        if command and command[0] == "--":
            command = command[1:]
        return execute_bundle(store, _bundle_request(args), command)
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


def _resolve_claim_credential(args: argparse.Namespace) -> None:
    """Resolve exactly one claim credential before opening durable state."""

    if hasattr(args, "token_file"):
        args.token = resolve_credential(
            token=getattr(args, "token", None),
            token_file=getattr(args, "token_file", None),
            token_fd=getattr(args, "token_fd", None),
        )


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

    if args.operation == "policy":
        args.operation = f"policy-{args.policy_operation}"

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
        _resolve_claim_credential(args)
        store = (
            None
            if args.operation in {"key", "policy-list", "policy-describe"}
            else LeaseStore(getattr(args, "home", None))
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
