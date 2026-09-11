"""Optional local stdio MCP server for the public Worklease API.

The MCP SDK is imported only when a server is created, so the core package
retains no runtime dependencies unless the ``mcp`` extra is installed.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import math
import os
import re
import secrets
import stat
import time
from collections.abc import Awaitable, Callable
from importlib.resources import files
from pathlib import Path
from typing import Any

from . import (
    DEFAULT_TTL,
    MAX_BUNDLE_RESOURCES,
    MAX_TTL,
    AcquireRequest,
    BundleAcquireRequest,
    BundleMutationRequest,
    LeaseError,
    LeaseFileState,
    LeaseStore,
    MutationRequest,
    agent_instructions,
    check_lease_file_writable,
    clear_lease_file,
    key_result,
    read_lease_file,
    require_bundle_resources,
    require_ttl,
    serialize_checkpoint,
    write_lease_file,
)

_HANDLE_RE = re.compile(r"^wl1-[0-9a-f]{32}-[0-9a-f]{32}$")
_AUTHORITY_RE = re.compile(r"^[0-9a-f]{32}$")
_DEFAULT_MAX_HOLD = 4 * 60 * 60.0
_AGENT_ID_ENV = "WORKLEASE_AGENT_ID"
_RETRYABLE_ACQUIRE_ERRORS = frozenset({"already-claimed", "resource-guarded"})

# Load the exact published schema so tool declarations cannot drift from the
# package artifact validated by clients and tests.
_OUTPUT_SCHEMA: dict[str, Any] = json.loads(
    files("worklease").joinpath("schemas", "v1", "mcp.json").read_text()
)


def _tool_schema(required: list[str], properties: dict[str, Any]) -> dict[str, Any]:
    return {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": properties,
        "required": required,
        "additionalProperties": False,
    }


_INPUT_SCHEMAS: dict[str, dict[str, Any]] = {
    "key": _tool_schema(
        ["provider", "source", "item"],
        {
            "provider": {"type": "string", "minLength": 1},
            "source": {"type": "string", "minLength": 1},
            "item": {"type": "string", "minLength": 1},
            "coordination_only": {"type": "boolean", "default": False},
        },
    ),
    "acquire": _tool_schema(
        ["resources"],
        {
            "resources": {
                "type": "array",
                "items": {"type": "string", "minLength": 1},
                "minItems": 1,
                "maxItems": MAX_BUNDLE_RESOURCES,
                "uniqueItems": True,
            },
            "ttl": {"type": "number", "exclusiveMinimum": 0},
            "work_key": {"type": "string", "minLength": 1},
            "coordination_only": {"type": "boolean", "default": False},
            "wait_timeout": {"type": "number", "minimum": 0, "maximum": 60},
            "auto_heartbeat": {"type": "boolean", "default": True},
            "max_hold": {"type": "number", "exclusiveMinimum": 0},
        },
    ),
    "status": _tool_schema(
        ["resources"],
        {
            "resources": {
                "type": "array",
                "items": {"type": "string", "minLength": 1},
                "minItems": 1,
                "maxItems": MAX_BUNDLE_RESOURCES,
                "uniqueItems": True,
            },
            "verbose": {"type": "boolean", "default": False},
        },
    ),
    "list": _tool_schema([], {"resource": {"type": "string", "minLength": 1}}),
    "heartbeat": _tool_schema(
        ["lease"],
        {
            "lease": {"type": "string", "pattern": _HANDLE_RE.pattern},
            "ttl": {"type": "number", "exclusiveMinimum": 0},
        },
    ),
    "checkpoint": _tool_schema(
        ["lease", "checkpoint"],
        {
            "lease": {"type": "string", "pattern": _HANDLE_RE.pattern},
            "checkpoint": {"type": "object"},
            "ttl": {"type": "number", "exclusiveMinimum": 0},
        },
    ),
    "release": _tool_schema(
        ["lease", "reason"],
        {
            "lease": {"type": "string", "pattern": _HANDLE_RE.pattern},
            "reason": {"type": "string", "minLength": 1},
        },
    ),
}


class _Runtime:
    def __init__(
        self, reference: str, ttl: float, max_hold: float, status: str
    ) -> None:
        self.reference = reference
        self.ttl = ttl
        self.max_hold = max_hold
        self.started = asyncio.get_running_loop().time()
        self.lock = asyncio.Lock()
        self.wake = asyncio.Event()
        self.status = status
        self.task: asyncio.Task[None] | None = None


class WorkleaseMCPServer:
    """Expose exactly seven typed tools over one local authority."""

    def __init__(
        self, home: str | os.PathLike[str] | None = None, *, agent_id: str | None = None
    ) -> None:
        try:
            from mcp.server import Server
        except ModuleNotFoundError as error:  # pragma: no cover - packaging guard
            raise RuntimeError(
                "install worklease[mcp] to use the MCP server"
            ) from error

        resolved_home = os.fspath(home) if isinstance(home, os.PathLike) else home
        self.store = LeaseStore(resolved_home)
        selected_agent_id = agent_id or os.environ.get(_AGENT_ID_ENV)
        self.agent_id = (
            selected_agent_id.strip()
            if isinstance(selected_agent_id, str) and selected_agent_id.strip()
            else "worklease-mcp"
        )
        self.session_id = secrets.token_hex(16)
        self._runtimes: dict[str, _Runtime] = {}
        self._locks: dict[str, asyncio.Lock] = {}
        self._busy: set[str] = set()
        self._handles = self.store.home / "mcp-leases"
        self._prepare_handles()
        instructions = "\n".join(
            (
                "Worklease agent instructions:",
                *agent_instructions("loop"),
                "",
                "Safety:",
                *agent_instructions("safety"),
            )
        )
        self.mcp = Server("worklease", instructions=instructions)
        self._register_handlers()

    def _prepare_handles(self) -> None:
        try:
            self._handles.mkdir(parents=True, exist_ok=True, mode=0o700)
            metadata = self._handles.lstat()
        except OSError as error:
            raise LeaseError("mcp-handle-directory-unsafe", code=64) from error
        if (
            stat.S_ISLNK(metadata.st_mode)
            or not stat.S_ISDIR(metadata.st_mode)
            or metadata.st_uid != os.geteuid()
        ):
            raise LeaseError("mcp-handle-directory-unsafe", code=64)
        self._handles.chmod(0o700)

        authority = self._handles / ".authority"
        try:
            metadata = authority.lstat()
        except FileNotFoundError:
            self._authority = secrets.token_hex(16)
            flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
            try:
                descriptor = os.open(authority, flags, 0o600)
                with os.fdopen(descriptor, "w", encoding="ascii") as target:
                    target.write(self._authority + "\n")
                    target.flush()
                    os.fsync(target.fileno())
            except OSError as error:
                raise LeaseError("mcp-authority-unsafe", code=64) from error
            return
        except OSError as error:
            raise LeaseError("mcp-authority-unsafe", code=64) from error
        if (
            stat.S_ISLNK(metadata.st_mode)
            or not stat.S_ISREG(metadata.st_mode)
            or metadata.st_uid != os.geteuid()
        ):
            raise LeaseError("mcp-authority-unsafe", code=64)
        authority.chmod(0o600)
        try:
            value = authority.read_text(encoding="ascii").strip()
        except (OSError, UnicodeError) as error:
            raise LeaseError("mcp-authority-malformed", code=64) from error
        if _AUTHORITY_RE.fullmatch(value) is None:
            raise LeaseError("mcp-authority-malformed", code=64)
        self._authority = value

    def _register_handlers(self) -> None:
        from mcp.shared.exceptions import McpError
        from mcp.types import METHOD_NOT_FOUND, ErrorData

        @self.mcp.list_tools()
        async def list_tools() -> list[Any]:
            return self.tools()

        @self.mcp.call_tool(validate_input=False)
        async def call_tool(name: str, arguments: dict[str, Any]) -> Any:
            if name not in self._tool_functions():
                raise McpError(
                    ErrorData(code=METHOD_NOT_FOUND, message=f"unknown tool: {name}")
                )
            return await self.call(name, arguments)

    def tools(self) -> list[Any]:
        """Return the complete public tool surface."""

        from mcp.types import Tool, ToolAnnotations

        definitions = (
            (
                "key",
                "Derive one stable provider-neutral resource key.",
                ToolAnnotations(readOnlyHint=True, idempotentHint=True),
            ),
            (
                "acquire",
                "Acquire one resource or one exact ordered resource bundle.",
                ToolAnnotations(),
            ),
            (
                "status",
                "Inspect one resource or one exact ordered resource bundle.",
                ToolAnnotations(readOnlyHint=True, idempotentHint=True),
            ),
            (
                "list",
                "List token-free claims, optionally filtered by resource.",
                ToolAnnotations(readOnlyHint=True, idempotentHint=True),
            ),
            (
                "heartbeat",
                "Renew a claim using an opaque persisted lease reference.",
                ToolAnnotations(),
            ),
            (
                "checkpoint",
                "Persist a bounded JSON-object checkpoint and renew a claim.",
                ToolAnnotations(),
            ),
            (
                "release",
                "Release a claim after authoritative provider verification.",
                ToolAnnotations(destructiveHint=True),
            ),
        )
        return [
            Tool(
                name=name,
                description=description,
                inputSchema=_INPUT_SCHEMAS[name],
                outputSchema=_OUTPUT_SCHEMA,
                annotations=annotations,
            )
            for name, description, annotations in definitions
        ]

    def _tool_functions(self) -> dict[str, Callable[..., Awaitable[Any]]]:
        return {
            "key": self.key,
            "acquire": self.acquire,
            "status": self.status,
            "list": self.list,
            "heartbeat": self.heartbeat,
            "checkpoint": self.checkpoint,
            "release": self.release,
        }

    async def call(self, name: str, arguments: dict[str, Any]) -> Any:
        """Invoke a tool directly through the same dispatch used by stdio."""

        function = self._tool_functions().get(name)
        if function is None:
            raise ValueError(name)
        if not isinstance(arguments, dict):
            return self._failure(name, LeaseError("invalid-arguments", code=64))
        try:
            return await function(**arguments)
        except TypeError:
            return self._failure(name, LeaseError("invalid-arguments", code=64))

    @staticmethod
    def _new_id() -> str:
        return secrets.token_hex(16)

    def _reference(self) -> tuple[str, Path]:
        reference = f"wl1-{self._authority}-{self._new_id()}"
        return reference, self._safe_path(reference)

    def _safe_path(self, reference: str) -> Path:
        path = self._handles / f"{reference}.lease"
        if (
            path.parent != self._handles
            or path.name != f"{reference}.lease"
            or path.resolve().parent != self._handles.resolve()
        ):
            raise LeaseError("mcp-handle-path-unsafe", code=64)
        return path

    def _path_for_reference(self, reference: Any) -> tuple[str, Path]:
        if not isinstance(reference, str) or _HANDLE_RE.fullmatch(reference) is None:
            raise LeaseError("invalid-lease-reference", code=64)
        authority = reference.split("-", 2)[1]
        if authority != self._authority:
            raise LeaseError("foreign-authority", code=64)
        return reference, self._safe_path(reference)

    def _read_handle(self, reference: Any) -> tuple[str, Path, LeaseFileState]:
        ref, path = self._path_for_reference(reference)
        return ref, path, read_lease_file(path)

    @staticmethod
    def _resources(value: Any) -> tuple[str, ...]:
        if not isinstance(value, list) or any(
            not isinstance(item, str) for item in value
        ):
            raise LeaseError("invalid-resources", code=64)
        return require_bundle_resources(value)

    @staticmethod
    def _ttl(value: Any) -> float:
        if value is None:
            return DEFAULT_TTL
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise LeaseError(
                "invalid-ttl",
                code=64,
                minimumExclusive=0,
                maximumInclusive=MAX_TTL,
            )
        return require_ttl(value)

    @staticmethod
    def _max_hold(value: Any) -> float:
        if value is None:
            return _DEFAULT_MAX_HOLD
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise LeaseError("invalid-max-hold", code=64)
        result = float(value)
        if not math.isfinite(result) or result <= 0:
            raise LeaseError("invalid-max-hold", code=64)
        return result

    @staticmethod
    def _wait_timeout(value: Any) -> float | None:
        if value is None:
            return None
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise LeaseError("invalid-wait-timeout", code=64, maximumInclusive=60)
        result = float(value)
        if not math.isfinite(result) or result < 0 or result > 60:
            raise LeaseError("invalid-wait-timeout", code=64, maximumInclusive=60)
        return result

    @staticmethod
    def _operation(
        operation: str,
        payload: dict[str, Any],
        *,
        reference: str | None = None,
        heartbeat: str | None = None,
    ) -> dict[str, Any]:
        result = {"schemaVersion": 1, "operation": operation, **payload}
        if reference is not None:
            result["lease"] = reference
        if heartbeat is not None:
            result["autoHeartbeat"] = heartbeat
        claim = result.get("claim")
        if reference is not None and isinstance(claim, dict):
            expires_at = claim.get("expiresAt")
            if isinstance(expires_at, str):
                result["expiresAt"] = expires_at
        return result

    @staticmethod
    def _redact(
        value: Any,
        secret: str | None = None,
        *,
        drop_checkpoints: bool = False,
    ) -> Any:
        if isinstance(value, dict):
            return {
                key: WorkleaseMCPServer._redact(
                    item, secret, drop_checkpoints=drop_checkpoints
                )
                for key, item in value.items()
                if key.lower() != "token"
                and not (drop_checkpoints and key.lower() == "checkpoint")
            }
        if isinstance(value, list):
            return [
                WorkleaseMCPServer._redact(
                    item, secret, drop_checkpoints=drop_checkpoints
                )
                for item in value
            ]
        if secret and isinstance(value, str):
            return value.replace(secret, "[REDACTED]")
        return value

    @classmethod
    def _result(
        cls,
        payload: dict[str, Any],
        *,
        error: bool = False,
        secret: str | None = None,
        drop_checkpoints: bool = False,
    ) -> Any:
        from mcp.types import CallToolResult, TextContent

        clean = cls._redact(payload, secret, drop_checkpoints=drop_checkpoints)
        operation = str(clean.get("operation", "unknown"))
        text = (
            f"OK {operation}"
            if clean.get("ok")
            else f"ERROR {operation}: {clean.get('error', 'unknown-error')}"
        )
        return CallToolResult(
            content=[TextContent(type="text", text=text)],
            structuredContent=clean,
            isError=error,
        )

    @classmethod
    def _failure(
        cls,
        operation: str,
        error: Exception,
        *,
        reference: str | None = None,
        heartbeat: str | None = None,
        secret: str | None = None,
    ) -> Any:
        if isinstance(error, LeaseError):
            payload: dict[str, Any] = {"ok": False, **error.as_dict()}
            hints = {
                "already-claimed": "Select another ready resource or retry briefly with wait_timeout.",
                "stale-claim": "Stop mutating and acquire a fresh claim.",
                "stale-revision": "Stop mutating and inspect the authority before recovery.",
                "claim-expired": "Stop work and acquire a fresh claim.",
                "invalid-token": "Use the original private lease reference.",
                "lease-busy": "Retry after the in-flight mutation completes.",
            }
            if error.reason in hints:
                payload["hint"] = hints[error.reason]
        else:
            payload = {
                "ok": False,
                "error": "storage-failure",
                "hint": "Inspect local authority availability before retrying.",
            }
        return cls._result(
            cls._operation(
                operation, payload, reference=reference, heartbeat=heartbeat
            ),
            error=True,
            secret=secret,
        )

    async def key(
        self,
        provider: Any = None,
        source: Any = None,
        item: Any = None,
        coordination_only: Any = False,
    ) -> Any:
        try:
            if not all(
                isinstance(value, str) and value.strip()
                for value in (provider, source, item)
            ) or not isinstance(coordination_only, bool):
                raise LeaseError("invalid-arguments", code=64)
            payload = key_result(
                provider, source, item, coordination_only=coordination_only
            )
            return self._result(self._operation("key", payload))
        except Exception as error:
            return self._failure("key", error)

    async def _acquire_with_wait(
        self,
        acquire: Callable[[], dict[str, Any]],
        timeout: float | None,
    ) -> dict[str, Any]:
        if timeout is None:
            return await asyncio.to_thread(acquire)
        deadline = time.monotonic() + timeout
        while True:
            try:
                return await asyncio.to_thread(acquire)
            except LeaseError as error:
                remaining = deadline - time.monotonic()
                if error.reason not in _RETRYABLE_ACQUIRE_ERRORS or remaining <= 0:
                    raise
                await asyncio.sleep(min(0.25, remaining))

    async def acquire(
        self,
        resources: Any = None,
        ttl: Any = None,
        work_key: Any = None,
        coordination_only: Any = False,
        wait_timeout: Any = None,
        auto_heartbeat: Any = True,
        max_hold: Any = None,
    ) -> Any:
        try:
            ordered = self._resources(resources)
            lifetime = self._ttl(ttl)
            if not isinstance(coordination_only, bool) or not isinstance(
                auto_heartbeat, bool
            ):
                raise LeaseError("invalid-arguments", code=64)
            timeout = self._wait_timeout(wait_timeout)
            hold = self._max_hold(max_hold)
            if work_key is None:
                work_key = (
                    ordered[0]
                    if len(ordered) == 1
                    else json.dumps(list(ordered), separators=(",", ":"))
                )
            if not isinstance(work_key, str) or not work_key.strip():
                raise LeaseError("invalid-work-key", code=64)

            reference, path = self._reference()
            check_lease_file_writable(path)
            claim_id = self._new_id()
            owner_id = self._new_id()
            if len(ordered) == 1:
                request = AcquireRequest(
                    ordered[0],
                    claim_id,
                    self.agent_id,
                    self.session_id,
                    owner_id,
                    work_key,
                    lifetime,
                    coordination_only,
                )
                payload = await self._acquire_with_wait(
                    lambda: self.store.acquire(request), timeout
                )
            else:
                request = BundleAcquireRequest(
                    ordered,
                    claim_id,
                    self.agent_id,
                    self.session_id,
                    owner_id,
                    work_key,
                    lifetime,
                    coordination_only,
                )
                payload = await self._acquire_with_wait(
                    lambda: self.store.acquire_bundle(request), timeout
                )
            claim = payload.get("claim")
            if not isinstance(claim, dict):
                raise LeaseError("claim-create-conflict", code=3)
            state = LeaseFileState.from_claim(claim)
            write_lease_file(path, state)
            status = "active" if auto_heartbeat else "disabled"
            runtime = _Runtime(reference, lifetime, hold, status)
            self._runtimes[reference] = runtime
            self._locks[reference] = runtime.lock
            if auto_heartbeat:
                runtime.task = asyncio.create_task(self._heartbeat_loop(runtime))
            return self._result(
                self._operation(
                    str(payload.get("operation", "acquire")),
                    payload,
                    reference=reference,
                    heartbeat=status,
                ),
                secret=state.token,
                # Recovery checkpoints can originate from non-MCP callers and
                # are not safe to project without inspecting their contents.
                drop_checkpoints=True,
            )
        except Exception as error:
            return self._failure("acquire", error)

    async def status(self, resources: Any = None, verbose: Any = False) -> Any:
        try:
            ordered = self._resources(resources)
            if not isinstance(verbose, bool):
                raise LeaseError("invalid-arguments", code=64)
            if len(ordered) == 1:
                function = self.store.status_verbose if verbose else self.store.status
                payload = await asyncio.to_thread(function, ordered[0])
            elif verbose:
                # Validate the exact ordered bundle before projecting the same
                # token-free diagnostics the CLI exposes for any bundle member.
                await asyncio.to_thread(self.store.bundle_status, ordered)
                payload = await asyncio.to_thread(self.store.status_verbose, ordered[0])
                payload["resources"] = list(ordered)
            else:
                payload = await asyncio.to_thread(self.store.bundle_status, ordered)
            return self._result(
                self._operation(str(payload.get("operation", "status")), payload),
                drop_checkpoints=True,
            )
        except Exception as error:
            return self._failure("status", error)

    async def list(self, resource: Any = None) -> Any:
        try:
            if resource is not None and (
                not isinstance(resource, str) or not resource.strip()
            ):
                raise LeaseError("invalid-resource", code=64)
            payload = await asyncio.to_thread(self.store.list_claims, resource)
            await asyncio.to_thread(self._cleanup_handles)
            return self._result(self._operation("list", payload), drop_checkpoints=True)
        except Exception as error:
            return self._failure("list", error)

    def _heartbeat_state(self, reference: str | None) -> str:
        if reference is None:
            return "stopped"
        runtime = self._runtimes.get(reference)
        return runtime.status if runtime is not None else "stopped"

    async def _mutate(
        self,
        operation: str,
        reference: Any,
        ttl: Any,
        action: Callable[[LeaseFileState, float], dict[str, Any]],
    ) -> Any:
        ref: str | None = None
        state: LeaseFileState | None = None
        try:
            ref, path, state = self._read_handle(reference)
            lifetime = self._ttl(ttl)
            if ref in self._busy:
                raise LeaseError("lease-busy", code=2)
            lock = self._locks.setdefault(ref, asyncio.Lock())
            self._busy.add(ref)
            try:
                async with lock:
                    _, current_path, current_state = self._read_handle(ref)
                    if current_path != path:
                        raise LeaseError("invalid-lease-reference", code=64)
                    check_lease_file_writable(path)
                    payload = await asyncio.to_thread(action, current_state, lifetime)
                    claim = payload.get("claim")
                    if isinstance(claim, dict):
                        next_state = LeaseFileState.from_claim(
                            claim, prior=current_state
                        )
                        # Idempotent operation replay can return an old receipt.
                        if next_state.revision >= current_state.revision:
                            write_lease_file(path, next_state)
                    runtime = self._runtimes.get(ref)
                    if runtime is not None and operation in {"heartbeat", "checkpoint"}:
                        runtime.ttl = lifetime
                        runtime.wake.set()
                    return self._result(
                        self._operation(
                            str(payload.get("operation", operation)),
                            payload,
                            reference=ref,
                            heartbeat=self._heartbeat_state(ref),
                        ),
                        secret=current_state.token,
                        drop_checkpoints=operation != "checkpoint",
                    )
            finally:
                self._busy.discard(ref)
        except Exception as error:
            return self._failure(
                operation,
                error,
                reference=ref,
                heartbeat=self._heartbeat_state(ref),
                secret=state.token if state is not None else None,
            )

    @staticmethod
    def _request(state: LeaseFileState, ttl: float, operation_id: str) -> Any:
        if state.is_bundle:
            assert state.resources is not None
            return BundleMutationRequest(
                state.resources,
                state.claim_id,
                state.token,
                state.revision,
                operation_id,
                ttl,
            )
        assert state.resource is not None
        return MutationRequest(
            state.resource,
            state.claim_id,
            state.token,
            state.revision,
            operation_id,
            ttl,
        )

    async def heartbeat(self, lease: Any = None, ttl: Any = None) -> Any:
        def action(state: LeaseFileState, lifetime: float) -> dict[str, Any]:
            request = self._request(state, lifetime, self._new_id())
            return (
                self.store.heartbeat_bundle(request)
                if isinstance(request, BundleMutationRequest)
                else self.store.heartbeat(request)
            )

        return await self._mutate("heartbeat", lease, ttl, action)

    async def checkpoint(
        self, lease: Any = None, checkpoint: Any = None, ttl: Any = None
    ) -> Any:
        def action(state: LeaseFileState, lifetime: float) -> dict[str, Any]:
            if not isinstance(checkpoint, dict):
                raise LeaseError("invalid-checkpoint", code=64)
            serialized = serialize_checkpoint(checkpoint)
            if state.token in serialized or any(
                word in key.lower()
                for key in self._json_keys(checkpoint)
                for word in ("token", "credential", "lease_file", "leasefile")
            ):
                raise LeaseError("secret-in-checkpoint", code=64)
            request = self._request(state, lifetime, self._new_id())
            return (
                self.store.checkpoint_bundle(request, checkpoint)
                if isinstance(request, BundleMutationRequest)
                else self.store.checkpoint(request, checkpoint)
            )

        return await self._mutate("checkpoint", lease, ttl, action)

    @classmethod
    def _json_keys(cls, value: Any) -> list[str]:
        if isinstance(value, dict):
            keys: list[str] = []
            for key, item in value.items():
                keys.append(str(key))
                keys.extend(cls._json_keys(item))
            return keys
        if isinstance(value, list):
            return [key for item in value for key in cls._json_keys(item)]
        return []

    async def release(self, lease: Any = None, reason: Any = None) -> Any:
        def action(state: LeaseFileState, lifetime: float) -> dict[str, Any]:
            if not isinstance(reason, str) or not reason.strip():
                raise LeaseError("invalid-release-reason", code=64)
            request = self._request(state, DEFAULT_TTL, self._new_id())
            return (
                self.store.release_bundle(request, reason)
                if isinstance(request, BundleMutationRequest)
                else self.store.release(request, reason)
            )

        result = await self._mutate("release", lease, DEFAULT_TTL, action)
        if not result.isError:
            assert isinstance(lease, str)
            ref, path = self._path_for_reference(lease)
            runtime = self._runtimes.pop(ref, None)
            if runtime is not None:
                runtime.status = "stopped"
                if runtime.task is not None:
                    runtime.task.cancel()
            result.structuredContent["autoHeartbeat"] = "stopped"
            # Authority release is already durable. On a handle error, leave
            # the stale private file available for operator inspection.
            with contextlib.suppress(LeaseError, OSError):
                clear_lease_file(path)
        return result

    async def _renew(self, reference: str, ttl: float) -> None:
        _, path, state = self._read_handle(reference)
        check_lease_file_writable(path)
        request = self._request(state, ttl, self._new_id())
        if isinstance(request, BundleMutationRequest):
            payload = await asyncio.to_thread(self.store.heartbeat_bundle, request)
        else:
            payload = await asyncio.to_thread(self.store.heartbeat, request)
        claim = payload.get("claim")
        if isinstance(claim, dict):
            write_lease_file(path, LeaseFileState.from_claim(claim, prior=state))

    async def _heartbeat_loop(self, runtime: _Runtime) -> None:
        try:
            while runtime.status == "active":
                elapsed = asyncio.get_running_loop().time() - runtime.started
                remaining = runtime.max_hold - elapsed
                if remaining <= 0:
                    runtime.status = "stopped"
                    return
                try:
                    await asyncio.wait_for(
                        runtime.wake.wait(),
                        timeout=min(runtime.ttl * 0.4, remaining),
                    )
                except TimeoutError:
                    pass
                else:
                    runtime.wake.clear()
                    continue
                elapsed = asyncio.get_running_loop().time() - runtime.started
                if runtime.status != "active" or elapsed >= runtime.max_hold:
                    runtime.status = "stopped"
                    return
                if runtime.reference in self._busy:
                    continue
                self._busy.add(runtime.reference)
                try:
                    async with runtime.lock:
                        await self._renew(runtime.reference, runtime.ttl)
                except Exception:
                    runtime.status = "stopped"
                    return
                finally:
                    self._busy.discard(runtime.reference)
        except asyncio.CancelledError:
            runtime.status = "stopped"
            raise

    def _cleanup_handles(self) -> None:
        """Remove handles only when their exact claim is no longer active."""

        for path in self._handles.glob("wl1-*.lease"):
            try:
                state = read_lease_file(path)
                if state.is_bundle:
                    assert state.resources is not None
                    payload = self.store.bundle_status(state.resources)
                else:
                    assert state.resource is not None
                    payload = self.store.status(state.resource)
                claim = payload.get("claim")
                active = (
                    payload.get("state") == "active"
                    and isinstance(claim, dict)
                    and claim.get("claimId") == state.claim_id
                    and claim.get("active") is True
                )
                if not active:
                    clear_lease_file(path)
            except LeaseError, OSError:
                continue

    async def shutdown(self) -> None:
        """Stop renewal tasks without releasing authority claims."""

        tasks: list[asyncio.Task[None]] = []
        for runtime in self._runtimes.values():
            runtime.status = "stopped"
            if runtime.task is not None:
                runtime.task.cancel()
                tasks.append(runtime.task)
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        self._runtimes.clear()
        self._locks.clear()
        self._busy.clear()

    async def serve_stdio(self) -> None:
        """Serve stdio until EOF, then stop every automatic heartbeat."""

        from mcp.server.stdio import stdio_server

        try:
            async with stdio_server() as (read_stream, write_stream):
                await self.mcp.run(
                    read_stream,
                    write_stream,
                    self.mcp.create_initialization_options(),
                )
        finally:
            await self.shutdown()

    def run(self) -> None:
        asyncio.run(self.serve_stdio())


def create_server(
    home: str | os.PathLike[str] | None = None, *, agent_id: str | None = None
) -> WorkleaseMCPServer:
    return WorkleaseMCPServer(home, agent_id=agent_id)


def main(argv: list[str] | None = None) -> int:
    """Run the local stdio server; stdout is reserved for MCP frames."""

    import argparse

    parser = argparse.ArgumentParser(prog="worklease-mcp")
    parser.add_argument(
        "--home", default=None, help="same state-home override as worklease"
    )
    parser.add_argument(
        "--agent-id", default=None, help=f"logical agent ID (default: {_AGENT_ID_ENV})"
    )
    args = parser.parse_args(argv)
    create_server(args.home, agent_id=args.agent_id).run()
    return 0


MCPServer = WorkleaseMCPServer

if __name__ == "__main__":
    raise SystemExit(main())

__all__ = ["MCPServer", "WorkleaseMCPServer", "create_server", "main"]
