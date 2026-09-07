"""Stateless and store-backed command dispatch for the CLI."""

from __future__ import annotations

import argparse
import json
import math
import time
from typing import Protocol

from .adapters import describe_policy, key_result, policy_descriptors
from .models import (
    DEFAULT_TTL,
    AcquireRequest,
    BundleAcquireRequest,
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
    TransferRequest,
)
from .store import LeaseStore

_DEFAULT_POLL_INTERVAL = 0.25
_RETRYABLE_ACQUIRE_ERRORS = frozenset({"already-claimed", "resource-guarded"})


def request_from_args(args: argparse.Namespace) -> MutationRequest:
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


def bundle_request_from_args(args: argparse.Namespace) -> BundleMutationRequest:
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


def dispatch_stateless(args: argparse.Namespace) -> tuple[dict[str, object], int]:
    """Dispatch commands that never open the lease store."""

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
    raise ValueError(operation)


def dispatch_store(
    args: argparse.Namespace, store: LeaseStore
) -> tuple[dict[str, object], int]:
    """Dispatch commands that require a durable lease store."""

    operation = args.operation
    if operation == "acquire":
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
        return store.bundle_status(tuple(args.resources)), 0
    if operation == "status":
        return (
            store.status_verbose(args.resource)
            if args.verbose
            else store.status(args.resource),
            0,
        )
    if operation == "history":
        return store.history(args.resource), 0
    if operation == "inspect-operation":
        return store.inspect_operation(args.resource, args.operation_id), 0
    if operation == "inspect-operation-bundle":
        return store.inspect_bundle_operation(
            tuple(args.resources), args.operation_id
        ), 0
    if operation == "gc":
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
        try:
            evidence = json.loads(args.evidence)
        except (TypeError, ValueError) as error:
            raise LeaseError("invalid-evidence", code=64) from error
        return (
            store.reconcile_operation(
                request_from_args(args),
                args.target_operation_id,
                args.expected_request_sha256,
                args.outcome,
                evidence,
            ),
            0,
        )
    if operation == "reconcile-operation-bundle":
        try:
            evidence = json.loads(args.evidence)
        except (TypeError, ValueError) as error:
            raise LeaseError("invalid-evidence", code=64) from error
        return (
            store.reconcile_bundle_operation(
                bundle_request_from_args(args),
                args.target_operation_id,
                args.expected_request_sha256,
                args.outcome,
                evidence,
            ),
            0,
        )
    if operation == "list":
        return store.list_claims(args.resource), 0
    if operation == "heartbeat":
        return store.heartbeat(request_from_args(args)), 0
    if operation == "checkpoint":
        try:
            checkpoint = json.loads(args.checkpoint)
        except (TypeError, ValueError) as error:
            raise LeaseError("invalid-checkpoint", code=64) from error
        return store.checkpoint(request_from_args(args), checkpoint), 0
    if operation == "transfer":
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
        return store.heartbeat_bundle(bundle_request_from_args(args)), 0
    if operation in {"release-bundle", "bundle-release"}:
        return store.release_bundle(bundle_request_from_args(args), args.reason), 0
    if operation == "release":
        return store.release(request_from_args(args), args.reason), 0
    if operation in {"exec-bundle", "bundle-exec"}:
        from .execution import execute_bundle

        command = list(args.command)
        if command and command[0] == "--":
            command = command[1:]
        return execute_bundle(
            store, bundle_request_from_args(args), command, args.max_duration
        )
    if operation == "exec":
        from .execution import execute

        command = list(args.command)
        if command and command[0] == "--":
            command = command[1:]
        return execute(store, request_from_args(args), command, args.max_duration)
    if operation == "replace-file":
        from .replacement import replace_file

        return (
            replace_file(
                store,
                request_from_args(args),
                args.path,
                args.expected_sha256,
                args.content_file,
            ),
            0,
        )
    raise ValueError(operation)


__all__ = [
    "_acquire_with_wait",
    "bundle_request_from_args",
    "dispatch_stateless",
    "dispatch_store",
    "request_from_args",
]
