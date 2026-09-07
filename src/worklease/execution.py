"""Guarded argv-only execution for opaque same-host leases."""

from __future__ import annotations

import hashlib
import os
import selectors
import signal
import sqlite3
import subprocess
import threading
import time
from collections.abc import Callable, Sequence
from contextlib import closing, suppress
from dataclasses import dataclass, replace
from typing import BinaryIO, cast

from .execution_context import provider_environment, resolve_execution_directory
from .locking import resource_lock, resource_locks
from .models import (
    DEFAULT_EXEC_MAX_DURATION,
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
    require_text,
)
from .store import LeaseStore

MAX_CAPTURE_BYTES = 1024 * 1024
EXEC_TIMEOUT_EXIT_CODE = 124
_READ_CHUNK_BYTES = 64 * 1024


@dataclass(slots=True)
class _BoundedCapture:
    """Drain one nonblocking pipe while retaining bounded receipt output."""

    stream: BinaryIO
    content: bytearray
    total_bytes: int = 0
    eof: bool = False

    def read_ready(self) -> None:
        try:
            chunk = os.read(self.stream.fileno(), _READ_CHUNK_BYTES)
        except BlockingIOError:
            return
        if not chunk:
            self.eof = True
            return
        self.total_bytes += len(chunk)
        remaining = MAX_CAPTURE_BYTES - len(self.content)
        if remaining > 0:
            self.content.extend(chunk[:remaining])

    def result(self) -> tuple[str, int, bool]:
        text = bytes(self.content).decode("utf-8", errors="replace")
        encoded = text.encode("utf-8")
        representation_truncated = len(encoded) > MAX_CAPTURE_BYTES
        if representation_truncated:
            text = encoded[:MAX_CAPTURE_BYTES].decode("utf-8", errors="ignore")
        return (
            text,
            self.total_bytes,
            self.total_bytes > len(self.content) or representation_truncated,
        )


def _capture_output(
    process: subprocess.Popen[bytes],
    renew: Callable[[], None],
    terminate: Callable[[subprocess.Popen[bytes]], None],
    interval: float,
    deadline: float,
    deadline_reached: threading.Event,
) -> tuple[tuple[str, int, bool], tuple[str, int, bool], bool]:
    """Drain child pipes without blocking past the execution deadline."""

    assert process.stdout is not None
    assert process.stderr is not None
    captures = (
        _BoundedCapture(cast(BinaryIO, process.stdout), bytearray()),
        _BoundedCapture(cast(BinaryIO, process.stderr), bytearray()),
    )
    for capture in captures:
        os.set_blocking(capture.stream.fileno(), False)

    timed_out = False
    next_renewal = time.monotonic() + interval
    with selectors.DefaultSelector() as selector:
        for capture in captures:
            selector.register(capture.stream, selectors.EVENT_READ, capture)

        while process.poll() is None or any(not capture.eof for capture in captures):
            if deadline_reached.is_set():
                timed_out = True
                break
            now = time.monotonic()
            remaining = deadline - now
            if remaining <= 0:
                deadline_reached.set()
                timed_out = True
                terminate(process)
                break
            wait_interval = min(remaining, max(0, next_renewal - now))
            for key, _ in selector.select(wait_interval):
                capture = cast(_BoundedCapture, key.data)
                capture.read_ready()
                if capture.eof:
                    selector.unregister(capture.stream)
                    capture.stream.close()
            if deadline_reached.is_set() or time.monotonic() >= deadline:
                if not deadline_reached.is_set():
                    deadline_reached.set()
                    terminate(process)
                timed_out = True
                break
            if (
                process.poll() is None or any(not capture.eof for capture in captures)
            ) and time.monotonic() >= next_renewal:
                renew()
                next_renewal = time.monotonic() + interval

        if timed_out:
            for capture in captures:
                if capture.eof:
                    continue
                capture.read_ready()
                with suppress(KeyError):
                    selector.unregister(capture.stream)
                capture.stream.close()

    return captures[0].result(), captures[1].result(), timed_out


def _command_receipt(
    argv: list[str],
    returncode: int,
    stdout: tuple[str, int, bool],
    stderr: tuple[str, int, bool],
    execution_directory: dict[str, str],
) -> dict[str, object]:
    return {
        "argv": argv,
        "returncode": returncode,
        "stdout": stdout[0],
        "stderr": stderr[0],
        "stdoutBytes": stdout[1],
        "stderrBytes": stderr[1],
        "stdoutTruncated": stdout[2],
        "stderrTruncated": stderr[2],
        "executionDirectory": execution_directory,
    }


class GuardedExecutor:
    """Run one command while renewing and validating its ownership epoch."""

    def __init__(self, store: LeaseStore) -> None:
        self.store = store

    @staticmethod
    def _validate_command(command: Sequence[str]) -> list[str]:
        values = list(command)
        if not values or not isinstance(values[0], str) or not values[0]:
            raise LeaseError("missing-command", code=64)
        if any(not isinstance(value, str) for value in values):
            raise LeaseError("invalid-command", code=64)
        return values

    @staticmethod
    def _validate_maximum_duration(maximum_duration: float) -> float:
        try:
            value = float(maximum_duration)
        except (OverflowError, TypeError, ValueError) as error:
            raise LeaseError("invalid-max-duration", code=64) from error
        if not 0 < value <= threading.TIMEOUT_MAX:
            raise LeaseError("invalid-max-duration", code=64)
        return value

    @staticmethod
    def _close_pipes(process: subprocess.Popen[bytes]) -> None:
        for stream in (process.stdout, process.stderr):
            if stream is not None:
                with suppress(OSError):
                    stream.close()

    @classmethod
    def _terminate(
        cls,
        process: subprocess.Popen[bytes],
        *,
        grace_period: float = 2,
        close_pipes: bool = True,
    ) -> None:
        # A completed parent can still have descendants holding our pipes open.
        # On POSIX the dedicated process group is the ownership boundary.
        try:
            if os.name == "posix":
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                except ProcessLookupError, PermissionError:
                    return
                try:
                    if process.poll() is None and grace_period > 0:
                        process.wait(timeout=grace_period)
                except subprocess.TimeoutExpired:
                    pass
                with suppress(ProcessLookupError, PermissionError):
                    # wait() only observes the group leader. Escalate for any
                    # descendant that ignores SIGTERM or holds inherited pipes.
                    os.killpg(process.pid, signal.SIGKILL)
                with suppress(ChildProcessError):
                    process.wait()
                return

            if process.poll() is None:
                process.terminate()
                try:
                    if grace_period > 0:
                        process.wait(timeout=grace_period)
                except subprocess.TimeoutExpired:
                    pass
                if process.poll() is None:
                    process.kill()
                    process.wait()
        finally:
            if close_pipes:
                cls._close_pipes(process)

    @classmethod
    def _terminate_at_deadline(cls, process: subprocess.Popen[bytes]) -> None:
        # Buffered pipe close can block behind a reader when a descendant escaped
        # the process group. Daemon drains may finish later, but the guard returns.
        cls._terminate(process, grace_period=0, close_pipes=False)

    @classmethod
    def _expire(
        cls, process: subprocess.Popen[bytes], deadline_reached: threading.Event
    ) -> None:
        try:
            cls._terminate_at_deadline(process)
        finally:
            deadline_reached.set()

    def execute(
        self,
        request: MutationRequest,
        command: Sequence[str],
        maximum_duration: float = DEFAULT_EXEC_MAX_DURATION,
    ) -> tuple[dict[str, object], int]:
        """Execute an argv without a shell and persist an idempotent receipt."""

        argv = self._validate_command(command)
        maximum_duration = self._validate_maximum_duration(maximum_duration)
        execution_directory = resolve_execution_directory(
            request.provider_directory, git_primary=request.git_primary
        )
        ttl = request.ttl
        operation_request = request.request_dict(
            argv=argv,
            executionDirectory=execution_directory.request_value(),
            maxDuration=maximum_duration,
        )
        with (
            resource_lock(request.resource, self.store.home),
            closing(self.store._connect()) as db,
            self.store._connection_context(db, internal_heartbeats=True),
        ):
            cached = self.store.begin_operation(
                request,
                "exec",
                operation_request,
                lock_held=True,
                connection=db,
            )
            if cached is not None:
                command_result = cached.get("command")
                if not isinstance(command_result, dict):
                    raise LeaseError("invalid-operation-receipt", code=3)
                return cached, int(command_result.get("returncode", 1))

            current_request = request
            heartbeat_count = 0
            process: subprocess.Popen[bytes] | None = None
            timed_out = False
            deadline_reached = threading.Event()

            def renew() -> None:
                nonlocal current_request, heartbeat_count
                heartbeat_request = replace(
                    current_request,
                    operation_id=(
                        f"{request.operation_id}:heartbeat:{heartbeat_count}"
                    ),
                )
                heartbeat = self.store._heartbeat_for_exec(
                    heartbeat_request, lock_held=True
                )
                claim = heartbeat.get("claim")
                if not isinstance(claim, dict):
                    raise LeaseError(
                        "invalid-heartbeat-receipt",
                        code=3,
                        resource=request.resource,
                    )
                current_request = replace(
                    current_request,
                    revision=int(claim["revision"]),
                )
                heartbeat_count += 1

            try:
                renew()
                process = subprocess.Popen(
                    argv,
                    stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    start_new_session=(os.name == "posix"),
                    cwd=(
                        str(execution_directory.path)
                        if execution_directory.path is not None
                        else None
                    ),
                    env=provider_environment()
                    if execution_directory.provider
                    else None,
                )
                interval = max(float(ttl) / 3, 1e-6)
                interval = min(interval, 5.0)
                deadline = time.monotonic() + maximum_duration
                watchdog = threading.Timer(
                    maximum_duration, self._expire, (process, deadline_reached)
                )
                watchdog.daemon = True
                watchdog.start()
                try:
                    stdout, stderr, timed_out = _capture_output(
                        process,
                        renew,
                        self._terminate_at_deadline,
                        interval,
                        deadline,
                        deadline_reached,
                    )
                finally:
                    watchdog.cancel()
                    watchdog.join()
                if not timed_out:
                    self._close_pipes(process)
                renew()
            except LeaseError:
                if process is not None:
                    self._terminate(process, close_pipes=not timed_out)
                raise
            except OSError as error:
                if process is not None:
                    self._terminate(process)
                    raise LeaseError(
                        "unknown-outcome",
                        code=3,
                        operationId=request.operation_id,
                        operation="exec",
                    ) from error
                stdout = ("", 0, False)
                message = str(error)
                stderr = (message, len(message.encode("utf-8")), False)
                returncode = 127
            except sqlite3.Error as error:
                if process is not None:
                    self._terminate(process)
                    raise LeaseError(
                        "unknown-outcome",
                        code=3,
                        operationId=request.operation_id,
                        operation="exec",
                    ) from error
                raise LeaseError(
                    "storage-failure",
                    code=75,
                    operationId=request.operation_id,
                    operation="heartbeat",
                ) from error
            except BaseException:
                if process is not None:
                    self._terminate(process)
                raise
            else:
                assert process is not None
                if timed_out:
                    returncode = EXEC_TIMEOUT_EXIT_CODE
                else:
                    returncode = int(process.returncode)
                    if returncode < 0:
                        returncode = 128 + (-returncode)

            command_receipt = _command_receipt(
                argv,
                returncode,
                stdout,
                stderr,
                execution_directory.request_value(),
            )
            receipt: dict[str, object] = {
                "ok": returncode == 0,
                "operation": "exec",
                "operationId": request.operation_id,
                "idempotent": False,
                "command": command_receipt,
                "ttl": float(ttl),
                "maxDuration": maximum_duration,
                "guarantee": "local-coordination",
                "providerFencing": False,
            }
            if timed_out:
                receipt["error"] = "child-process-timeout"
                command_receipt["timedOut"] = True
            elif returncode != 0:
                receipt["error"] = "child-process-failed"
            completed = self.store.complete_operation(
                current_request,
                "exec",
                operation_request,
                receipt,
                lock_held=True,
                connection=db,
            )
            return completed, returncode

    def execute_bundle(
        self,
        request: BundleMutationRequest,
        command: Sequence[str],
        maximum_duration: float = DEFAULT_EXEC_MAX_DURATION,
    ) -> tuple[dict[str, object], int]:
        """Execute one argv while renewing an all-member bundle claim."""

        argv = self._validate_command(command)
        maximum_duration = self._validate_maximum_duration(maximum_duration)
        execution_directory = resolve_execution_directory(
            request.provider_directory, git_primary=request.git_primary
        )
        operation_request = request.request_dict(
            argv=argv,
            executionDirectory=execution_directory.request_value(),
            maxDuration=maximum_duration,
        )
        operation_request["tokenHash"] = hashlib.sha256(
            request.token.encode("utf-8")
        ).hexdigest()
        with (
            resource_locks(request.resources, self.store.home),
            closing(self.store._connect()) as db,
            self.store._connection_context(db, internal_heartbeats=True),
        ):
            cached = self.store.begin_bundle_operation(
                request,
                "exec-bundle",
                operation_request,
                lock_held=True,
                connection=db,
            )
            if cached is not None:
                command_result = cached.get("command")
                if not isinstance(command_result, dict):
                    raise LeaseError("invalid-operation-receipt", code=3)
                return cached, int(command_result.get("returncode", 1))

            current_request = request
            heartbeat_count = 0
            process: subprocess.Popen[bytes] | None = None
            timed_out = False
            deadline_reached = threading.Event()

            def renew() -> None:
                nonlocal current_request, heartbeat_count
                heartbeat_request = replace(
                    current_request,
                    operation_id=(
                        f"{request.operation_id}:heartbeat:{heartbeat_count}"
                    ),
                )
                heartbeat = self.store._heartbeat_for_exec(
                    heartbeat_request, lock_held=True
                )
                claim = heartbeat.get("claim")
                if not isinstance(claim, dict):
                    raise LeaseError(
                        "invalid-heartbeat-receipt",
                        code=3,
                        resource=",".join(request.resources),
                    )
                current_request = replace(
                    current_request,
                    revision=int(claim["revision"]),
                )
                heartbeat_count += 1

            try:
                renew()
                process = subprocess.Popen(
                    argv,
                    stdin=subprocess.DEVNULL,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    start_new_session=(os.name == "posix"),
                    cwd=(
                        str(execution_directory.path)
                        if execution_directory.path is not None
                        else None
                    ),
                    env=provider_environment()
                    if execution_directory.provider
                    else None,
                )
                interval = max(float(request.ttl) / 3, 1e-6)
                interval = min(interval, 5.0)
                deadline = time.monotonic() + maximum_duration
                watchdog = threading.Timer(
                    maximum_duration, self._expire, (process, deadline_reached)
                )
                watchdog.daemon = True
                watchdog.start()
                try:
                    stdout, stderr, timed_out = _capture_output(
                        process,
                        renew,
                        self._terminate_at_deadline,
                        interval,
                        deadline,
                        deadline_reached,
                    )
                finally:
                    watchdog.cancel()
                    watchdog.join()
                if not timed_out:
                    self._close_pipes(process)
                renew()
            except LeaseError:
                if process is not None:
                    self._terminate(process, close_pipes=not timed_out)
                raise
            except OSError as error:
                if process is not None:
                    self._terminate(process)
                    raise LeaseError(
                        "unknown-outcome",
                        code=3,
                        operationId=request.operation_id,
                        operation="exec-bundle",
                    ) from error
                stdout = ("", 0, False)
                message = str(error)
                stderr = (message, len(message.encode("utf-8")), False)
                returncode = 127
            except sqlite3.Error as error:
                if process is not None:
                    self._terminate(process)
                    raise LeaseError(
                        "unknown-outcome",
                        code=3,
                        operationId=request.operation_id,
                        operation="heartbeat-bundle",
                    ) from error
                raise LeaseError(
                    "storage-failure",
                    code=75,
                    operationId=request.operation_id,
                    operation="heartbeat-bundle",
                ) from error
            except BaseException:
                if process is not None:
                    self._terminate(process)
                raise
            else:
                assert process is not None
                if timed_out:
                    returncode = EXEC_TIMEOUT_EXIT_CODE
                else:
                    returncode = int(process.returncode)
                    if returncode < 0:
                        returncode = 128 + (-returncode)

            command_receipt = _command_receipt(
                argv,
                returncode,
                stdout,
                stderr,
                execution_directory.request_value(),
            )
            receipt: dict[str, object] = {
                "ok": returncode == 0,
                "operation": "exec-bundle",
                "operationId": request.operation_id,
                "idempotent": False,
                "resources": list(request.resources),
                "command": command_receipt,
                "ttl": float(request.ttl),
                "maxDuration": maximum_duration,
                "guarantee": "local-coordination",
                "providerFencing": False,
            }
            if timed_out:
                receipt["error"] = "child-process-timeout"
                command_receipt["timedOut"] = True
            elif returncode != 0:
                receipt["error"] = "child-process-failed"
            completed = self.store.complete_bundle_operation(
                current_request,
                "exec-bundle",
                operation_request,
                receipt,
                lock_held=True,
                connection=db,
            )
            return completed, returncode


def execute(
    store: LeaseStore,
    request: MutationRequest,
    command: Sequence[str],
    maximum_duration: float = DEFAULT_EXEC_MAX_DURATION,
) -> tuple[dict[str, object], int]:
    """Convenience wrapper around :class:`GuardedExecutor`."""

    require_text(request.operation_id, "operation-id")
    return GuardedExecutor(store).execute(request, command, maximum_duration)


def execute_bundle(
    store: LeaseStore,
    request: BundleMutationRequest,
    command: Sequence[str],
    maximum_duration: float = DEFAULT_EXEC_MAX_DURATION,
) -> tuple[dict[str, object], int]:
    """Convenience wrapper for guarded execution over a bundle."""

    require_text(request.operation_id, "operation-id")
    return GuardedExecutor(store).execute_bundle(request, command, maximum_duration)
