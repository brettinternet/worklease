"""Command-line interface for the worklease package."""

from __future__ import annotations

import argparse
import json
import math
import os
import re
import sys
import time
from collections.abc import Sequence
from typing import Any, NoReturn, Protocol, cast

from . import __version__
from ._release_metadata import PUBLISHED_RELEASE_VERSION
from .adapters import (
    describe_policy,
    key_result,
    policy_descriptors,
)
from .credentials import resolve_credential
from .execution import execute, execute_bundle
from .models import (
    DEFAULT_TTL,
    AcquireRequest,
    BundleAcquireRequest,
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
    TransferRequest,
)
from .replacement import replace_file
from .store import DEFAULT_GC_RETENTION_DAYS, LeaseStore

_REPOSITORY_URL = "https://github.com/brettinternet/worklease"
_PUBLISHED_RELEASE_VERSION_ENV = "WORKLEASE_PUBLISHED_RELEASE_VERSION"
_CANONICAL_DOCS_REF = "main"
_RELEASE_VERSION_PATTERN = re.compile(
    r"(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)"
    r"(?:[-+][0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
)


def _documentation_url(path: str, *, release_version: str | None = None) -> str:
    """Build a documentation URL from explicit published release metadata."""

    ref = (
        f"v{release_version}"
        if release_version and _RELEASE_VERSION_PATTERN.fullmatch(release_version)
        else _CANONICAL_DOCS_REF
    )
    return f"{_REPOSITORY_URL}/blob/{ref}/{path}"


def _agent_workflow_guidance() -> str:
    """Return stable onboarding guidance for workflow-oriented agents."""

    release_version = (
        os.environ.get(_PUBLISHED_RELEASE_VERSION_ENV) or PUBLISHED_RELEASE_VERSION
    )
    return f"""\
{_help_heading("Agent workflow:")}
  Workflow semantics and source/provider coordination:
    {_documentation_url("skills/worklease-workflow/SKILL.md", release_version=release_version)}
  Project documentation and installation:
    {_documentation_url("README.md", release_version=release_version)}
  Use `worklease COMMAND --help` for command syntax and options.
  Automation must request schema-versioned JSON with `--json` (or `--format json`)."""


_DEFAULT_HOME_HELP = (
    "default: WORKLEASE_HOME, then XDG_STATE_HOME/worklease, "
    "then ~/.local/state/worklease"
)

_DEFAULT_CUTOFF_HELP = "default: derived from --retention-days"


class _ExplicitValueAction(argparse.Action):
    """Record when an option was supplied instead of using its default."""

    def __call__(
        self,
        parser: argparse.ArgumentParser,
        namespace: argparse.Namespace,
        values: Any,
        option_string: str | None = None,
    ) -> None:
        setattr(namespace, self.dest, values)
        setattr(namespace, f"_{self.dest}_provided", True)


_DEFAULT_POLL_INTERVAL = 0.25

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
        "inspect-operation-bundle",
        "reconcile-operation",
        "reconcile-operation-bundle",
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


_HELP_HEADING_START = "\x1eH\x1f"
_HELP_COMMAND_START = "\x1eC\x1f"
_HELP_COLOR_END = "\x1eE\x1f"


def _help_heading(value: str) -> str:
    return f"{_HELP_HEADING_START}{value}{_HELP_COLOR_END}"


def _help_command(value: str) -> str:
    return f"{_HELP_COMMAND_START}{value}{_HELP_COLOR_END}"


class _ColorizedHelpFormatter(argparse.RawDescriptionHelpFormatter):
    """Preserve argparse colors in the manually grouped command epilog."""

    def _fill_text(self, text: str, width: int, indent: str) -> str:
        theme: Any = cast(Any, self)._theme
        text = text.replace(_HELP_HEADING_START, theme.heading)
        text = text.replace(_HELP_COMMAND_START, theme.action)
        text = text.replace(_HELP_COLOR_END, theme.reset)
        return super()._fill_text(text, width, indent)


class _ArgumentParser(argparse.ArgumentParser):
    """ArgumentParser variant that leaves output formatting to :func:`main`."""

    def __init__(self, *args: Any, **kwargs: Any) -> None:
        kwargs.setdefault("allow_abbrev", False)
        kwargs.setdefault("formatter_class", _ColorizedHelpFormatter)
        super().__init__(*args, **kwargs)

    def error(self, message: str) -> NoReturn:
        raise _ArgumentError(message)


_TOP_LEVEL_EPILOG = f"""\
{_help_heading("Command groups:")}
  {_help_heading("Singleton:")}
    {_help_command("key")}                 derive one stable resource key
    {_help_command("acquire")}             atomically acquire or reclaim a lease
    {_help_command("status")}              read current lease state
    {_help_command("list")}                list current and expired claims
    {_help_command("heartbeat")}           renew an active lease
    {_help_command("checkpoint")}          persist a bounded JSON checkpoint and renew a lease
    {_help_command("exec")}                run one argv under a lease
    {_help_command("release")}             release an active lease
    {_help_command("transfer")}            atomically transfer an active lease to a successor
    {_help_command("replace-file")}        atomically replace a file by expected hash
  {_help_heading("Bundles:")}
    {_help_command("acquire-bundle (bundle-acquire)")}
                        atomically acquire or reclaim an opaque resource bundle
    {_help_command("status-bundle (bundle-status, inspect-bundle)")}
                        inspect an exact resource bundle
    {_help_command("heartbeat-bundle (bundle-heartbeat)")}
                        renew every member of an active bundle
    {_help_command("exec-bundle (bundle-exec)")}
                        run one argv under an opaque bundle claim
    {_help_command("release-bundle (bundle-release)")}
                        release every member of an active bundle
  {_help_heading("Inspection and reconciliation:")}
    {_help_command("inspect-operation")}   inspect one operation outcome
    {_help_command("reconcile-operation")}
                        record an observed operation outcome
    {_help_command("inspect-operation-bundle")}
                        inspect one ordered bundle operation outcome
    {_help_command("reconcile-operation-bundle")}
                        record an observed ordered bundle operation outcome
  {_help_heading("Maintenance:")}
    {_help_command("policy")}              inspect available resource-key policies
    {_help_command("gc")}                  inspect or collect records eligible for garbage collection

{_help_heading("Examples:")}
  worklease key --provider backlog-md --source docs/backlog --item TASK-42
  worklease status --resource local:formatter

{_agent_workflow_guidance()}"""


def _single_line_epilog(example: str) -> str:
    return f"""\
Example:
  {example}"""


_KEY_EPILOG = _single_line_epilog(
    "worklease key --provider backlog-md --source docs/backlog --item TASK-42"
)
_POLICY_EPILOG = _single_line_epilog("worklease policy list")
_POLICY_LIST_EPILOG = _single_line_epilog("worklease policy list")
_POLICY_DESCRIBE_EPILOG = _single_line_epilog(
    "worklease policy describe --name generic"
)
_ACQUIRE_BUNDLE_EPILOG = """\
Example:
  worklease acquire-bundle \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --agent-id agent-1 \\
    --session-id session-1 \\
    --owner-id attempt-1 \\
    --work-key format:repo"""
_STATUS_BUNDLE_EPILOG = _single_line_epilog(
    "worklease status-bundle --resource local:formatter"
)
_STATUS_EPILOG = _single_line_epilog("worklease status --resource local:formatter")
_INSPECT_OPERATION_EPILOG = _single_line_epilog(
    "worklease inspect-operation --resource local:formatter "
    "--operation-id test-TASK-42-001"
)
_INSPECT_OPERATION_BUNDLE_EPILOG = _single_line_epilog(
    "worklease inspect-operation-bundle --resource local:formatter "
    "--operation-id test-TASK-42-001"
)
_GC_EPILOG = _single_line_epilog("worklease gc")
_RECONCILE_OPERATION_EPILOG = """\
Example:
  worklease reconcile-operation \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --token "$TOKEN" \\
    --revision "$REVISION" \\
    --operation-id "reconcile-TASK-42-001" \\
    --target-operation-id "test-TASK-42-001" \\
    --expected-request-sha256 "$EXPECTED_REQUEST_SHA256" \\
    --outcome observed-success \\
    --evidence '{}'"""
_RECONCILE_OPERATION_BUNDLE_EPILOG = """\
Example:
  worklease reconcile-operation-bundle \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --token "$TOKEN" \\
    --revision "$REVISION" \\
    --operation-id "reconcile-TASK-42-001" \\
    --target-operation-id "test-TASK-42-001" \\
    --expected-request-sha256 "$EXPECTED_REQUEST_SHA256" \\
    --outcome observed-success \\
    --evidence '{}'"""
_CHECKPOINT_EPILOG = """\
Example:
  worklease checkpoint \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --token "$TOKEN" \\
    --revision "$REVISION" \\
    --operation-id "checkpoint-TASK-42-001" \\
    --checkpoint '{"step":1}'"""
_TRANSFER_EPILOG = """\
Example:
  worklease transfer \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --token "$TOKEN" \\
    --revision "$REVISION" \\
    --operation-id "transfer-TASK-42-001" \\
    --successor-claim-id claim-successor \\
    --successor-agent-id agent-2 \\
    --successor-session-id session-2 \\
    --successor-owner-id attempt-2 \\
    --successor-work-key format:repo"""
_LIST_EPILOG = _single_line_epilog("worklease list")
_HEARTBEAT_EPILOG = """\
Example:
  worklease heartbeat \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --token "$TOKEN" \\
    --revision "$REVISION" \\
    --operation-id 'heartbeat-TASK-42-001'"""
_HEARTBEAT_BUNDLE_EPILOG = """\
Example:
  worklease heartbeat-bundle \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --token "$TOKEN" \\
    --revision "$REVISION" \\
    --operation-id 'heartbeat-TASK-42-001'"""
_RELEASE_BUNDLE_EPILOG = """\
Example:
  worklease release-bundle \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --token "$TOKEN" \\
    --revision "$REVISION" \\
    --operation-id "release-TASK-42-001" \\
    --reason 'provider checkpoint verified'"""
_EXEC_BUNDLE_EPILOG = """\
Example:
  worklease exec-bundle \\
    --resource "$RESOURCE" \\
    --claim-id "$CLAIM_ID" \\
    --token-file "$TOKEN_FILE" \\
    --revision "$REVISION" \\
    --operation-id "test-TASK-42-001" \\
    -- python -m unittest discover -s tests -v"""


_ACQUIRE_EPILOG = """\
Example:
  worklease acquire \\
    --resource local:formatter \\
    --claim-id claim-formatter \\
    --agent-id agent-1 \\
    --session-id session-1 \\
    --owner-id attempt-1 \\
    --work-key format:repo \\
    --ttl 900"""


_EXEC_EPILOG = """\
Example:
  worklease exec \\
    --resource "$RESOURCE" \\
    --claim-id "$CLAIM_ID" \\
    --token-file "$TOKEN_FILE" \\
    --revision "$REVISION" \\
    --operation-id "test-TASK-42-001" \\
    -- python -m unittest discover -s tests -v"""


_RELEASE_EPILOG = """\
Example:
  worklease release \\
    --resource "$RESOURCE" \\
    --claim-id "$CLAIM_ID" \\
    --token-file "$TOKEN_FILE" \\
    --revision "$REVISION" \\
    --operation-id "release-TASK-42-001" \\
    --reason 'provider checkpoint verified'"""


_REPLACE_FILE_EPILOG = """\
Example:
  worklease replace-file \\
    --resource "$RESOURCE" \\
    --claim-id "$CLAIM_ID" \\
    --token-file "$TOKEN_FILE" \\
    --revision "$REVISION" \\
    --operation-id "replace-TASK-42-001" \\
    --path docs/backlog/TASK-42.md \\
    --expected-sha256 "$EXPECTED_SHA256" \\
    --content-file /tmp/TASK-42.md"""


def _add_output_arguments(
    parser: argparse.ArgumentParser, *, include_format: bool = True
) -> None:
    if include_format:
        parser.add_argument(
            "-f",
            "--format",
            choices=("json", "text"),
            default=argparse.SUPPRESS,
            help="output format (default: text)",
        )

    parser.add_argument(
        "-j",
        "--json",
        action="store_true",
        default=argparse.SUPPRESS,
        help="output JSON (equivalent to --format json)",
    )
    parser.add_argument(
        "-H",
        "--home",
        default=argparse.SUPPRESS,
        help=f"override WORKLEASE_HOME for this command ({_DEFAULT_HOME_HELP})",
    )


def _add_ttl_argument(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "-T",
        "--ttl",
        default=DEFAULT_TTL,
        type=float,
        help=f"lease lifetime in seconds (default: {DEFAULT_TTL:g})",
    )


def _common_claim_arguments(
    parser: argparse.ArgumentParser, *, include_ttl: bool = True
) -> None:
    parser.add_argument("-r", "--resource", required=True)
    parser.add_argument("-c", "--claim-id", required=True)
    parser.add_argument("-t", "--token")
    parser.add_argument("-F", "--token-file")
    parser.add_argument("-D", "--token-fd")
    parser.add_argument("-R", "--revision", required=True, type=int)
    parser.add_argument("-o", "--operation-id", required=True)
    if include_ttl:
        _add_ttl_argument(parser)


def _execution_directory_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "-d",
        "--provider-directory",
        help="run provider commands from this validated working directory "
        "(default: caller directory)",
    )
    parser.add_argument(
        "-g",
        "--git-primary",
        action="store_true",
        help="derive the registered Git primary/control worktree "
        "(default: caller directory)",
    )


def _bundle_resources(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "-r",
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
    parser.add_argument("-c", "--claim-id", required=True)
    parser.add_argument("-t", "--token")
    parser.add_argument("-F", "--token-file")
    parser.add_argument("-D", "--token-fd")
    parser.add_argument("-R", "--revision", required=True, type=int)
    parser.add_argument("-o", "--operation-id", required=True)
    if include_ttl:
        _add_ttl_argument(parser)


class _GroupedSubparsers(argparse._SubParsersAction):
    """Leave top-level command grouping to the parser epilog."""

    def _get_subactions(self) -> list[argparse.Action]:
        return []


def _canonical_subparsers(
    action: argparse._SubParsersAction,
) -> list[tuple[str, argparse.ArgumentParser, tuple[str, ...]]]:
    """Return each parser once, preserving canonical names and aliases."""

    parsers: list[tuple[str, argparse.ArgumentParser, tuple[str, ...]]] = []
    by_parser: dict[int, int] = {}
    for name, child in action.choices.items():
        parser_index = by_parser.get(id(child))
        if parser_index is None:
            by_parser[id(child)] = len(parsers)
            parsers.append((name, child, ()))
        else:
            canonical, parser, aliases = parsers[parser_index]
            parsers[parser_index] = (canonical, parser, (*aliases, name))
    return parsers


def _aggregate_help(parser: argparse.ArgumentParser) -> str:
    """Render deterministic help for the parser tree."""

    sections: list[str] = []

    def add_section(
        command_path: tuple[str, ...],
        command_parser: argparse.ArgumentParser,
        aliases: tuple[str, ...] = (),
    ) -> None:
        body = command_parser.format_help().rstrip()
        alias_text = ", ".join(aliases) if aliases else "(none)"
        sections.append(
            f"=== {' '.join(command_path)} ===\n{body}\nAliases: {alias_text}"
        )
        for action in command_parser._actions:
            if isinstance(action, argparse._SubParsersAction):
                for name, child, child_aliases in _canonical_subparsers(action):
                    add_section(
                        (*command_path, name),
                        child,
                        child_aliases,
                    )

    add_section((parser.prog,), parser)
    return "\n\n".join(sections) + "\n"


def _parser() -> _ArgumentParser:
    parser = _ArgumentParser(prog="worklease", epilog=_TOP_LEVEL_EPILOG)
    parser.add_argument(
        "-f",
        "--format",
        choices=("json", "text"),
        default="text",
        help="output format (default: text)",
    )
    parser.add_argument(
        "-j",
        "--json",
        action="store_true",
        default=argparse.SUPPRESS,
        help="output JSON (equivalent to --format json)",
    )
    parser.add_argument(
        "-H",
        "--home",
        default=None,
        help=f"override WORKLEASE_HOME for this command ({_DEFAULT_HOME_HELP})",
    )
    parser.add_argument(
        "-v",
        "--version",
        action="store_true",
        help="print the packaged worklease version",
    )
    parser.add_argument(
        "-a",
        "--help-all",
        action="store_true",
        help="show help for every canonical command",
    )
    commands = parser.add_subparsers(
        dest="operation",
        parser_class=_ArgumentParser,
        metavar="COMMAND",
        action=_GroupedSubparsers,
    )

    key_parser = commands.add_parser(
        "key", help="derive one stable resource key", epilog=_KEY_EPILOG
    )
    _add_output_arguments(key_parser)
    key_parser.add_argument("-p", "--provider", required=True)
    key_parser.add_argument("-s", "--source", required=True)
    key_parser.add_argument("-i", "--item", required=True)
    key_parser.add_argument(
        "-C",
        "--coordination-only",
        action="store_true",
        help="derive a lease that coordinates local workers without fencing writes",
    )

    policy_parser = commands.add_parser(
        "policy",
        help="inspect available resource-key policies",
        epilog=_POLICY_EPILOG,
    )
    policy_commands = policy_parser.add_subparsers(
        dest="policy_operation", required=True, parser_class=_ArgumentParser
    )
    list_policy_parser = policy_commands.add_parser(
        "list", help="list available resource-key policies", epilog=_POLICY_LIST_EPILOG
    )
    _add_output_arguments(list_policy_parser)
    describe_parser = policy_commands.add_parser(
        "describe",
        help="describe one resource-key policy",
        epilog=_POLICY_DESCRIBE_EPILOG,
    )
    _add_output_arguments(describe_parser)
    describe_parser.add_argument("-n", "--name", required=True)

    acquire_parser = commands.add_parser(
        "acquire", help="atomically acquire or reclaim a lease", epilog=_ACQUIRE_EPILOG
    )
    _add_output_arguments(acquire_parser)
    acquire_parser.add_argument("-r", "--resource", required=True)
    acquire_parser.add_argument("-c", "--claim-id", required=True)
    acquire_parser.add_argument("-a", "--agent-id", required=True)
    acquire_parser.add_argument("-s", "--session-id", required=True)
    acquire_parser.add_argument("-o", "--owner-id", required=True)
    acquire_parser.add_argument("-w", "--work-key", required=True)
    acquire_parser.add_argument(
        "-C",
        "--coordination-only",
        action="store_true",
        help="mark this ownership epoch as unable to fence provider writes",
    )
    acquire_parser.add_argument(
        "-W",
        "--wait-timeout",
        type=float,
        default=None,
        help=(
            "retry singleton acquisition for at most SECONDS "
            "(default: one immediate attempt; no retries)"
        ),
    )
    acquire_parser.add_argument(
        "-P",
        "--poll-interval",
        type=float,
        default=None,
        help=(
            f"seconds between singleton acquisition retries "
            f"(default: {_DEFAULT_POLL_INTERVAL:g} seconds with --wait-timeout; "
            "invalid without --wait-timeout)"
        ),
    )
    _add_ttl_argument(acquire_parser)

    acquire_bundle_parser = commands.add_parser(
        "acquire-bundle",
        aliases=("bundle-acquire",),
        help="atomically acquire or reclaim an opaque resource bundle",
        epilog=_ACQUIRE_BUNDLE_EPILOG,
    )
    _add_output_arguments(acquire_bundle_parser)
    _bundle_resources(acquire_bundle_parser)
    acquire_bundle_parser.add_argument("-c", "--claim-id", required=True)
    acquire_bundle_parser.add_argument("-a", "--agent-id", required=True)
    acquire_bundle_parser.add_argument("-s", "--session-id", required=True)
    acquire_bundle_parser.add_argument("-o", "--owner-id", required=True)
    acquire_bundle_parser.add_argument("-w", "--work-key", required=True)
    acquire_bundle_parser.add_argument("-C", "--coordination-only", action="store_true")
    _add_ttl_argument(acquire_bundle_parser)

    status_bundle_parser = commands.add_parser(
        "status-bundle",
        aliases=("bundle-status", "inspect-bundle"),
        help="inspect an exact resource bundle",
        epilog=_STATUS_BUNDLE_EPILOG,
    )
    _add_output_arguments(status_bundle_parser)
    _bundle_resources(status_bundle_parser)

    status_parser = commands.add_parser(
        "status", help="read current lease state", epilog=_STATUS_EPILOG
    )
    _add_output_arguments(status_parser)
    status_parser.add_argument("-r", "--resource", required=True)
    status_parser.add_argument(
        "-V",
        "--verbose",
        action="store_true",
        help="include redacted diagnostic metadata and unknown outcomes",
    )

    inspect_operation_parser = commands.add_parser(
        "inspect-operation",
        help="inspect one operation outcome",
        epilog=_INSPECT_OPERATION_EPILOG,
    )
    _add_output_arguments(inspect_operation_parser)
    inspect_operation_parser.add_argument("-r", "--resource", required=True)
    inspect_operation_parser.add_argument("-o", "--operation-id", required=True)
    inspect_bundle_operation_parser = commands.add_parser(
        "inspect-operation-bundle",
        help="inspect one ordered bundle operation outcome",
        epilog=_INSPECT_OPERATION_BUNDLE_EPILOG,
    )
    _add_output_arguments(inspect_bundle_operation_parser)
    _bundle_resources(inspect_bundle_operation_parser)
    inspect_bundle_operation_parser.add_argument("-o", "--operation-id", required=True)
    gc_parser = commands.add_parser(
        "gc",
        help="inspect or collect records eligible for garbage collection",
        epilog=_GC_EPILOG,
    )
    _add_output_arguments(gc_parser)
    gc_parser.add_argument(
        "-r",
        "--retention-days",
        action=_ExplicitValueAction,
        default=DEFAULT_GC_RETENTION_DAYS,
        type=float,
        help=(
            "retain records newer than this many days "
            f"(default: {DEFAULT_GC_RETENTION_DAYS:g})"
        ),
    )
    gc_parser.add_argument(
        "-c",
        "--cutoff",
        help=f"explicit UTC cutoff timestamp (ISO-8601; {_DEFAULT_CUTOFF_HELP})",
    )
    gc_parser.add_argument(
        "-a",
        "--apply",
        action="store_true",
        help=(
            "atomically delete records eligible under the selected cutoff "
            "(default: dry run)"
        ),
    )
    reconcile_operation_parser = commands.add_parser(
        "reconcile-operation",
        help="record an observed operation outcome",
        epilog=_RECONCILE_OPERATION_EPILOG,
    )
    _add_output_arguments(reconcile_operation_parser)
    _common_claim_arguments(reconcile_operation_parser)
    reconcile_operation_parser.add_argument("--target-operation-id", required=True)
    reconcile_operation_parser.add_argument(
        "-x", "--expected-request-sha256", required=True
    )
    reconcile_operation_parser.add_argument(
        "-O",
        "--outcome",
        required=True,
        choices=("observed-success", "observed-failure"),
    )
    reconcile_operation_parser.add_argument("-e", "--evidence", required=True)
    reconcile_bundle_operation_parser = commands.add_parser(
        "reconcile-operation-bundle",
        help="record an observed ordered bundle operation outcome",
        epilog=_RECONCILE_OPERATION_BUNDLE_EPILOG,
    )
    _add_output_arguments(reconcile_bundle_operation_parser)
    _common_bundle_claim_arguments(reconcile_bundle_operation_parser)
    reconcile_bundle_operation_parser.add_argument(
        "--target-operation-id", required=True
    )
    reconcile_bundle_operation_parser.add_argument(
        "-x", "--expected-request-sha256", required=True
    )
    reconcile_bundle_operation_parser.add_argument(
        "-O",
        "--outcome",
        required=True,
        choices=("observed-success", "observed-failure"),
    )
    reconcile_bundle_operation_parser.add_argument("-e", "--evidence", required=True)

    checkpoint_parser = commands.add_parser(
        "checkpoint",
        help="persist a bounded JSON checkpoint and renew a lease",
        epilog=_CHECKPOINT_EPILOG,
    )
    _add_output_arguments(checkpoint_parser)
    _common_claim_arguments(checkpoint_parser)
    checkpoint_parser.add_argument("-k", "--checkpoint", required=True)

    transfer_parser = commands.add_parser(
        "transfer",
        help="atomically transfer an active lease to a successor",
        epilog=_TRANSFER_EPILOG,
    )
    _add_output_arguments(transfer_parser)
    transfer_parser.add_argument("-r", "--resource", required=True)
    transfer_parser.add_argument("-c", "--claim-id", required=True)
    transfer_parser.add_argument("-t", "--token")
    transfer_parser.add_argument("-F", "--token-file")
    transfer_parser.add_argument("-D", "--token-fd")
    transfer_parser.add_argument("-R", "--revision", required=True, type=int)
    transfer_parser.add_argument("-o", "--operation-id", required=True)
    transfer_parser.add_argument("-C", "--successor-claim-id", required=True)
    transfer_parser.add_argument("-A", "--successor-agent-id", required=True)
    transfer_parser.add_argument("-S", "--successor-session-id", required=True)
    transfer_parser.add_argument("-O", "--successor-owner-id", required=True)
    transfer_parser.add_argument("-W", "--successor-work-key", required=True)
    _add_ttl_argument(transfer_parser)

    list_parser = commands.add_parser(
        "list", help="list current and expired claims", epilog=_LIST_EPILOG
    )
    _add_output_arguments(list_parser)
    list_parser.add_argument(
        "-r",
        "--resource",
        help="filter claims to one exact resource (default: all resources)",
    )
    list_parser.add_argument(
        "-F",
        "--full",
        action="store_true",
        help="show full resources, identifiers, and absolute expiry timestamps",
    )

    heartbeat_parser = commands.add_parser(
        "heartbeat", help="renew an active lease", epilog=_HEARTBEAT_EPILOG
    )
    _add_output_arguments(heartbeat_parser)
    _common_claim_arguments(heartbeat_parser)

    release_parser = commands.add_parser(
        "release", help="release an active lease", epilog=_RELEASE_EPILOG
    )
    _add_output_arguments(release_parser)
    _common_claim_arguments(release_parser, include_ttl=False)
    release_parser.add_argument("-m", "--reason", required=True)

    execute_parser = commands.add_parser(
        "exec", help="run one argv under a lease", epilog=_EXEC_EPILOG
    )
    _add_output_arguments(execute_parser)
    _common_claim_arguments(execute_parser)
    _execution_directory_arguments(execute_parser)
    execute_parser.add_argument("command", nargs=argparse.REMAINDER)

    heartbeat_bundle_parser = commands.add_parser(
        "heartbeat-bundle",
        aliases=("bundle-heartbeat",),
        help="renew every member of an active bundle",
        epilog=_HEARTBEAT_BUNDLE_EPILOG,
    )
    _add_output_arguments(heartbeat_bundle_parser)
    _common_bundle_claim_arguments(heartbeat_bundle_parser)

    release_bundle_parser = commands.add_parser(
        "release-bundle",
        aliases=("bundle-release",),
        help="release every member of an active bundle",
        epilog=_RELEASE_BUNDLE_EPILOG,
    )
    _add_output_arguments(release_bundle_parser)
    _common_bundle_claim_arguments(release_bundle_parser, include_ttl=False)
    release_bundle_parser.add_argument("-m", "--reason", required=True)

    execute_bundle_parser = commands.add_parser(
        "exec-bundle",
        aliases=("bundle-exec",),
        help="run one argv under an opaque bundle claim",
        epilog=_EXEC_BUNDLE_EPILOG,
    )
    _add_output_arguments(execute_bundle_parser)
    _common_bundle_claim_arguments(execute_bundle_parser)
    _execution_directory_arguments(execute_bundle_parser)
    execute_bundle_parser.add_argument("command", nargs=argparse.REMAINDER)
    replace_parser = commands.add_parser(
        "replace-file",
        help="atomically replace a file by expected hash",
        epilog=_REPLACE_FILE_EPILOG,
    )
    _add_output_arguments(replace_parser)
    _common_claim_arguments(replace_parser)
    replace_parser.add_argument("-p", "--path", required=True)
    replace_parser.add_argument("-e", "--expected-sha256", required=True)
    replace_parser.add_argument("-C", "--content-file", required=True)
    return parser


def _text_value(value: object) -> str:
    """Render one text-mode scalar without allowing control-character injection."""

    rendered = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    return "".join(
        (f"\\u{ord(character):04x}" if 0x7F <= ord(character) <= 0x9F else character)
        for character in rendered
    )


def _text_atom(value: object) -> str:
    """Keep simple labels readable while escaping control characters."""

    text = "" if value is None else str(value)
    if any(
        ord(character) < 0x20 or 0x7F <= ord(character) <= 0x9F for character in text
    ):
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
        "available",
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
    for field in (
        "returncode",
        "executionDirectory",
        "stdoutBytes",
        "stdoutTruncated",
        "stderrBytes",
        "stderrTruncated",
    ):
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
        "genericExecutionGuarantee",
        "providerFencingSupported",
    )
    for field in fields:
        if field in payload:
            print(f"{field}: {_text_atom(payload[field])}")


def _render_table(headers: tuple[str, ...], rows: list[tuple[str, ...]]) -> None:
    all_rows = (headers, *rows)
    widths = tuple(
        max(len(row[index]) for row in all_rows) for index in range(len(headers))
    )
    for row in all_rows:
        padded = [
            cell.ljust(width) for cell, width in zip(row[:-1], widths[:-1], strict=True)
        ]
        padded.append(row[-1])
        print("   ".join(padded))


_LIST_RESOURCE_WIDTH = 52
_LIST_CLAIM_ID_WIDTH = 18
_LIST_OWNER_ID_WIDTH = 24
_LIST_EXPIRY_WIDTH = 16


def _shorten_text(value: str, width: int) -> str:
    """Keep a readable prefix and suffix within a fixed display width."""

    if len(value) <= width:
        return value
    if width <= 1:
        return value[:width]
    prefix_width = (width - 1) // 2
    suffix_width = width - 1 - prefix_width
    return f"{value[:prefix_width]}…{value[-suffix_width:]}"


def _relative_duration(seconds: float) -> str:
    """Render a non-negative approximate duration for a list row."""

    total_seconds = max(0, int(seconds))
    if total_seconds < 60:
        return f"{total_seconds}s"
    total_minutes, _ = divmod(total_seconds, 60)
    if total_minutes < 60:
        return f"{total_minutes}m"
    total_hours, minutes = divmod(total_minutes, 60)
    if total_hours < 24:
        return f"{total_hours}h {minutes}m" if minutes else f"{total_hours}h"
    days, hours = divmod(total_hours, 24)
    return f"{days}d {hours}h" if hours else f"{days}d"


def _compact_expiry(claim: dict[str, object], now: float) -> str:
    """Render an expiry as a short relative value without negative durations."""

    expires_value = claim.get("expiresAtEpoch")
    if not isinstance(expires_value, (int, float, str)):
        return _shorten_text(_text_atom(claim.get("expiresAt", "")), _LIST_EXPIRY_WIDTH)
    try:
        expires_at = float(expires_value)
    except ValueError:
        return _shorten_text(_text_atom(claim.get("expiresAt", "")), _LIST_EXPIRY_WIDTH)
    remaining = expires_at - now
    if remaining < 0:
        return _shorten_text(
            f"expired {_relative_duration(-remaining)}", _LIST_EXPIRY_WIDTH
        )
    if not claim.get("active", True):
        return "expired"
    return _shorten_text(_relative_duration(remaining), _LIST_EXPIRY_WIDTH)


def _list_resource(claim: dict[str, object], *, full: bool) -> str:
    resource = claim.get("resource")
    value = (
        _text_atom(resource)
        if resource is not None
        else _text_value(claim.get("resources", []))
    )
    return value if full else _shorten_text(value, _LIST_RESOURCE_WIDTH)


def _render_list(
    payload: dict[str, object], *, full: bool = False, now: float | None = None
) -> None:
    if not payload.get("ok"):
        _text_header(payload)
        _emit_error_details(payload)
        return
    display_now = time.time() if now is None else now
    rows: list[tuple[str, ...]] = []
    claims = payload.get("claims", [])
    if isinstance(claims, list):
        for claim in claims:
            if not isinstance(claim, dict):
                continue
            rows.append(
                (
                    "active" if claim.get("active") else "expired",
                    _list_resource(claim, full=full),
                    _text_atom(claim.get("claimId", ""))
                    if full
                    else _shorten_text(
                        _text_atom(claim.get("claimId", "")), _LIST_CLAIM_ID_WIDTH
                    ),
                    _text_atom(claim.get("ownerId", ""))
                    if full
                    else _shorten_text(
                        _text_atom(claim.get("ownerId", "")), _LIST_OWNER_ID_WIDTH
                    ),
                    _text_atom(claim.get("expiresAt", ""))
                    if full
                    else _compact_expiry(claim, display_now),
                )
            )
    _render_table(
        ("STATE", "RESOURCE", "CLAIM_ID", "OWNER_ID", "EXPIRES_AT"),
        rows,
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
        "resources",
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
    "inspect-operation-bundle": _render_inspect_operation,
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
    "reconcile-operation-bundle": _render_mutation,
}


def _emit(
    payload: dict[str, object], output_format: str, *, full: bool = False
) -> None:
    if output_format == "text":
        operation = str(payload.get("operation", "unknown"))
        if operation == "list":
            _render_list(payload, full=full)
        else:
            renderer = _TEXT_RENDERERS.get(operation, _render_generic)
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


def _command_help_path(argv: Sequence[str]) -> str:
    options = _visible_output_options(argv)
    for index, value in enumerate(options):
        if value == "policy":
            if index + 1 < len(options) and options[index + 1] in {"list", "describe"}:
                return f"worklease policy {options[index + 1]}"
            return "worklease policy"
        if value in _COMMANDS:
            return f"worklease {value}"
    return "worklease"


def _parser_error_hint(argv: Sequence[str], message: str) -> str | None:
    command = _command_help_path(argv)
    choice_match = re.search(
        r"argument (?:-[\w-]+/)?(--[\w-]+): invalid choice: .* \(choose from (.+)\)",
        message,
    )
    if choice_match:
        choices = re.findall(r"'([^']+)'", choice_match.group(2))
        if choices:
            return f"Valid values for {choice_match.group(1)}: " + ", ".join(choices)

    expected_match = re.search(
        r"argument (?:-[\w-]+/)?(--[\w-]+): expected one argument",
        message,
    )
    if expected_match:
        option = expected_match.group(1)
        if command == "worklease status" and option == "--resource":
            return "Example: worklease status --resource local:formatter"
        return f"Provide a value for {option}; see {command} --help"

    required_match = re.search(
        r"the following arguments are required: (.+)",
        message,
    )
    if required_match:
        options = re.findall(r"--[\w-]+", required_match.group(1))
        if options:
            if command == "worklease status" and "--resource" in options:
                return "Example: worklease status --resource local:formatter"
            return "Required options: " + ", ".join(options) + f"; see {command} --help"

    if (
        "unrecognized arguments" in message
        or "invalid int value" in message
        or "invalid float value" in message
    ):
        return f"See {command} --help for required options and examples"
    return None


def _emit_parser_hint(argv: Sequence[str], message: str) -> None:
    hint = _parser_error_hint(argv, message)
    if hint is not None:
        print(f"HINT\t{_text_atom(hint)}")


def _emit_runtime_error_hint(operation: str, reason: str, output_format: str) -> None:
    if (
        output_format == "text"
        and operation == "status"
        and reason == "invalid-resource"
    ):
        print("HINT\tExample: worklease status --resource local:formatter")


def _request(args: argparse.Namespace) -> MutationRequest:
    return MutationRequest(
        resource=args.resource,
        claim_id=args.claim_id,
        token=args.token,
        revision=args.revision,
        operation_id=args.operation_id,
        ttl=getattr(args, "ttl", DEFAULT_TTL),
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
        ttl=getattr(args, "ttl", DEFAULT_TTL),
        provider_directory=getattr(args, "provider_directory", None),
        git_primary=getattr(args, "git_primary", False),
    )


class _AcquireStore(Protocol):
    def acquire(self, request: AcquireRequest) -> dict[str, object]: ...


_RETRYABLE_ACQUIRE_ERRORS = frozenset({"already-claimed", "resource-guarded"})


def _validate_wait_options(
    wait_timeout: float | None, poll_interval: float | None
) -> tuple[float | None, float | None]:
    if wait_timeout is None:
        if poll_interval is not None:
            raise LeaseError("invalid-poll-interval", code=64)
        return None, None
    if not math.isfinite(wait_timeout) or wait_timeout < 0:
        raise LeaseError("invalid-wait-timeout", code=64)
    interval = _DEFAULT_POLL_INTERVAL if poll_interval is None else poll_interval
    if not math.isfinite(interval) or interval <= 0:
        raise LeaseError("invalid-poll-interval", code=64)
    return wait_timeout, interval


def _acquire_with_wait(
    store: _AcquireStore,
    request: AcquireRequest,
    wait_timeout: float | None,
    poll_interval: float | None,
    *,
    clock=time.monotonic,
    sleeper=time.sleep,
) -> dict[str, object]:
    timeout, interval = _validate_wait_options(wait_timeout, poll_interval)
    if timeout is None:
        return store.acquire(request)
    assert interval is not None

    deadline = clock() + timeout
    while True:
        try:
            return store.acquire(request)
        except LeaseError as error:
            if error.reason not in _RETRYABLE_ACQUIRE_ERRORS:
                raise
            remaining = deadline - clock()
            if remaining <= 0:
                raise
            sleeper(min(interval, remaining))
            if clock() >= deadline:
                raise


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
            _acquire_with_wait(
                store,
                AcquireRequest(
                    resource=args.resource,
                    claim_id=args.claim_id,
                    agent_id=args.agent_id,
                    session_id=args.session_id,
                    owner_id=args.owner_id,
                    work_key=args.work_key,
                    ttl=args.ttl,
                    coordination_only=args.coordination_only,
                ),
                args.wait_timeout,
                args.poll_interval,
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
    if operation == "inspect-operation-bundle":
        assert store is not None
        return store.inspect_bundle_operation(
            tuple(args.resources), args.operation_id
        ), 0
    if operation == "gc":
        assert store is not None
        retention_days = getattr(args, "retention_days", None)
        if getattr(args, "cutoff", None) is not None and not getattr(
            args, "_retention_days_provided", False
        ):
            retention_days = None
        return (
            store.garbage_collect(
                retention_days=retention_days,
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
    if operation == "reconcile-operation-bundle":
        assert store is not None
        try:
            evidence = json.loads(args.evidence)
        except (TypeError, ValueError) as error:
            raise LeaseError("invalid-evidence", code=64) from error
        return (
            store.reconcile_bundle_operation(
                _bundle_request(args),
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


def _visible_output_options(argv: Sequence[str]) -> list[str]:
    options = list(argv)
    if "--" in options:
        options = options[: options.index("--")]
    return options


def _validate_output_arguments(argv: Sequence[str]) -> None:
    options = _visible_output_options(argv)
    has_json = "--json" in options or "-j" in options
    has_format = any(
        value in {"--format", "-f"}
        or value.startswith("--format=")
        or value.startswith("-f=")
        for value in options
    )
    if has_json and has_format:
        raise _ArgumentError("conflicting-output-format")


def _fallback_output_format(argv: Sequence[str]) -> str:
    """Choose a safe format for parser errors without reading child argv."""

    options = _visible_output_options(argv)
    explicit_format: str | None = None
    has_json = False
    for index, value in enumerate(options):
        if value in {"--json", "-j"}:
            has_json = True
        elif value in {"--format", "-f"} and index + 1 < len(options):
            explicit_format = options[index + 1]
        elif value.startswith("--format=") or value.startswith("-f="):
            explicit_format = value.partition("=")[2]
    if explicit_format in {"json", "text"}:
        return explicit_format
    if has_json:
        return "json"
    return "text"


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
        _validate_output_arguments(values)
        parser = _parser()
        args = parser.parse_args(values)
        output_format = "json" if getattr(args, "json", False) else args.format
    except _ArgumentError as error:
        _emit(
            _envelope(
                _operation_hint(values),
                {"ok": False, "error": "invalid-arguments"},
            ),
            output_format,
        )
        if output_format == "text":
            _emit_parser_hint(values, error.message)
        return 64
    if args.help_all:
        print(_aggregate_help(parser), end="")
        return 0

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
        _emit(output, output_format, full=getattr(args, "full", False))
        return child_code
    except LeaseError as error:
        output = _envelope(
            args.operation,
            {"ok": False, **error.as_dict()},
        )
        _emit(output, output_format, full=getattr(args, "full", False))
        _emit_runtime_error_hint(args.operation, error.reason, output_format)
        return error.code


if __name__ == "__main__":
    raise SystemExit(main())
