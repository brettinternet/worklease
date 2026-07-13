"""Guarded argv-only execution for opaque same-host leases."""

from __future__ import annotations

from dataclasses import replace
import os
import signal
import subprocess
from typing import Sequence

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
    def _terminate(process: subprocess.Popen[str]) -> None:
        if process.poll() is not None:
            return
        try:
            if os.name == "posix":
                os.killpg(process.pid, signal.SIGTERM)
            else:
                process.terminate()
            process.wait(timeout=2)
        except (ProcessLookupError, subprocess.TimeoutExpired):
            if process.poll() is None:
                try:
                    if os.name == "posix":
                        os.killpg(process.pid, signal.SIGKILL)
                    else:
                        process.kill()
                except ProcessLookupError:
                    pass
                process.wait()

    def execute(
        self, request: MutationRequest, command: Sequence[str]
    ) -> tuple[dict[str, object], int]:
        """Execute an argv without a shell and persist an idempotent receipt."""

        argv = self._validate_command(command)
        ttl = request.ttl
        operation_request = request.request_dict(argv=argv)
        cached = self.store.begin_operation(request, "exec", operation_request)
        if cached is not None:
            command_result = cached.get("command")
            if not isinstance(command_result, dict):
                raise LeaseError("invalid-operation-receipt", code=3)
            return cached, int(command_result.get("returncode", 1))

        current_request = request
        heartbeat_count = 0
        process: subprocess.Popen[str] | None = None
        try:
            process = subprocess.Popen(
                argv,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                start_new_session=(os.name == "posix"),
            )
            interval = max(0.01, min(float(ttl) / 3, 5.0))
            while True:
                try:
                    stdout, stderr = process.communicate(timeout=interval)
                    break
                except subprocess.TimeoutExpired:
                    heartbeat_request = replace(
                        current_request,
                        operation_id=(
                            f"{request.operation_id}:heartbeat:{heartbeat_count}"
                        ),
                    )
                    heartbeat = self.store.heartbeat(heartbeat_request)
                    claim = heartbeat.get("claim")
                    if not isinstance(claim, dict):
                        self._terminate(process)
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
        except LeaseError:
            if process is not None:
                self._terminate(process)
            raise
        except OSError as error:
            stdout = ""
            stderr = str(error)
            returncode = 127
        else:
            assert process is not None
            returncode = int(process.returncode)

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
        }
        completed = self.store.complete_operation(
            current_request, "exec", operation_request, receipt
        )
        return completed, returncode


def execute(
    store: LeaseStore, request: MutationRequest, command: Sequence[str]
) -> tuple[dict[str, object], int]:
    """Convenience wrapper around :class:`GuardedExecutor`."""

    require_text(request.operation_id, "operation-id")
    return GuardedExecutor(store).execute(request, command)
