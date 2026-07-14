"""Guarded argv-only execution for opaque same-host leases."""

from __future__ import annotations

import os
import signal
import sqlite3
import subprocess
from collections.abc import Sequence
from contextlib import suppress
from dataclasses import replace

from .locking import resource_lock
from .models import LeaseError, MutationRequest, require_text
from .store import LeaseStore


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
    def _close_pipes(process: subprocess.Popen[str]) -> None:
        for stream in (process.stdout, process.stderr):
            if stream is not None:
                with suppress(OSError):
                    stream.close()

    @classmethod
    def _terminate(cls, process: subprocess.Popen[str]) -> None:
        # A completed parent can still have descendants holding our pipes open.
        # On POSIX the dedicated process group is the ownership boundary.
        try:
            if os.name == "posix":
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                except ProcessLookupError, PermissionError:
                    return
                try:
                    if process.poll() is None:
                        process.wait(timeout=2)
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
                    process.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
        finally:
            cls._close_pipes(process)

    def execute(
        self, request: MutationRequest, command: Sequence[str]
    ) -> tuple[dict[str, object], int]:
        """Execute an argv without a shell and persist an idempotent receipt."""

        argv = self._validate_command(command)
        ttl = request.ttl
        operation_request = request.request_dict(argv=argv)
        with resource_lock(request.resource, self.store.home):
            cached = self.store.begin_operation(
                request, "exec", operation_request, lock_held=True
            )
            if cached is not None:
                command_result = cached.get("command")
                if not isinstance(command_result, dict):
                    raise LeaseError("invalid-operation-receipt", code=3)
                return cached, int(command_result.get("returncode", 1))

            current_request = request
            heartbeat_count = 0
            process: subprocess.Popen[str] | None = None

            def renew() -> None:
                nonlocal current_request, heartbeat_count
                heartbeat_request = replace(
                    current_request,
                    operation_id=(
                        f"{request.operation_id}:heartbeat:{heartbeat_count}"
                    ),
                )
                heartbeat = self.store.heartbeat(heartbeat_request, lock_held=True)
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
                    text=True,
                    encoding="utf-8",
                    errors="replace",
                    start_new_session=(os.name == "posix"),
                )
                interval = max(float(ttl) / 3, 1e-6)
                interval = min(interval, 5.0)
                while True:
                    try:
                        stdout, stderr = process.communicate(timeout=interval)
                        break
                    except subprocess.TimeoutExpired:
                        renew()
                renew()
            except LeaseError:
                if process is not None:
                    self._terminate(process)
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
                stdout = ""
                stderr = str(error)
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
                returncode = int(process.returncode)
                if returncode < 0:
                    returncode = 128 + (-returncode)

            command_receipt = {
                "argv": argv,
                "returncode": returncode,
                "stdout": stdout,
                "stderr": stderr,
            }
            receipt: dict[str, object] = {
                "ok": returncode == 0,
                "operation": "exec",
                "operationId": request.operation_id,
                "idempotent": False,
                "command": command_receipt,
                "ttl": float(ttl),
                "guarantee": "local-coordination",
                "providerFencing": False,
            }
            if returncode != 0:
                receipt["error"] = "child-process-failed"
            completed = self.store.complete_operation(
                current_request,
                "exec",
                operation_request,
                receipt,
                lock_held=True,
            )
            return completed, returncode


def execute(
    store: LeaseStore, request: MutationRequest, command: Sequence[str]
) -> tuple[dict[str, object], int]:
    """Convenience wrapper around :class:`GuardedExecutor`."""

    require_text(request.operation_id, "operation-id")
    return GuardedExecutor(store).execute(request, command)
