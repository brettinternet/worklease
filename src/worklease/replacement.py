"""Atomic expected-hash file replacement under an opaque lease."""

from __future__ import annotations

import hashlib
import os
import re
import stat
import tempfile
from pathlib import Path
from typing import cast

from .locking import resource_lock
from .models import LeaseError, MutationRequest, require_text
from .store import LeaseStore

_SHA256 = re.compile(r"^[0-9a-fA-F]{64}$")


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _regular_file(path: Path, *, field: str) -> Path:
    if path.is_symlink():
        raise LeaseError(f"{field}-is-symlink", code=64, path=str(path))
    try:
        resolved = path.expanduser().resolve(strict=True)
    except FileNotFoundError as error:
        raise LeaseError("file-not-found", code=64, path=str(path)) from error
    if resolved.is_symlink() or not resolved.is_file():
        raise LeaseError("file-not-found", code=64, path=str(resolved))
    return resolved


def atomic_replace(path: Path, content: bytes, mode: int) -> None:
    """Write, fsync, and rename a replacement while preserving file mode."""

    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{path.name}.worklease-", dir=path.parent
    )
    temporary = Path(temporary_name)
    try:
        os.fchmod(descriptor, mode)
        with os.fdopen(descriptor, "wb") as target:
            target.write(content)
            target.flush()
            os.fsync(target.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if temporary.exists():
            temporary.unlink()


class FileReplacer:
    """Perform one CAS-protected atomic replacement."""

    def __init__(self, store: LeaseStore) -> None:
        self.store = store

    def replace(
        self,
        request: MutationRequest,
        path: str | os.PathLike[str],
        expected_sha256: str,
        content_file: str | os.PathLike[str],
    ) -> dict[str, object]:
        expected = expected_sha256.lower()
        if not _SHA256.fullmatch(expected):
            raise LeaseError("invalid-expected-sha256", code=64)
        raw_target = Path(path).expanduser()
        raw_candidate = Path(content_file).expanduser()
        if raw_target.is_symlink():
            raise LeaseError("target-is-symlink", code=64, path=str(raw_target))
        try:
            requested_target = raw_target.resolve(strict=False)
            requested_candidate = raw_candidate.resolve(strict=False)
        except OSError as error:
            raise LeaseError(
                "file-unreadable",
                code=64,
                path=str(raw_target),
                contentFile=str(raw_candidate),
            ) from error
        candidate_hash: str | None = None
        if raw_candidate.is_file() and not raw_candidate.is_symlink():
            try:
                candidate_hash = hashlib.sha256(raw_candidate.read_bytes()).hexdigest()
            except OSError:
                candidate_hash = None

        cached = self.store.read_operation(request, "replace-file")
        if cached is not None:
            recorded = cast(dict[str, object], cached["request"])
            if (
                recorded.get("path") != str(requested_target)
                or recorded.get("previousSha256") != expected
                or (
                    recorded.get("contentFile") is not None
                    and recorded.get("contentFile") != str(requested_candidate)
                )
                or (
                    candidate_hash is not None
                    and recorded.get("sha256") != candidate_hash
                )
            ):
                raise LeaseError(
                    "operation-id-request-mismatch",
                    code=3,
                    operationId=request.operation_id,
                )
            receipt = cast(dict[str, object], cached["receipt"])
            if not bool(receipt.get("ok", False)):
                raise LeaseError(
                    str(receipt.get("error", "replace-file-failed")),
                    code=3,
                    operationId=request.operation_id,
                )
            return receipt

        try:
            target = _regular_file(raw_target, field="target")
            candidate = _regular_file(raw_candidate, field="content-file")
            content = candidate.read_bytes()
        except OSError as error:
            raise LeaseError(
                "file-unreadable",
                code=64,
                path=str(raw_target),
                contentFile=str(raw_candidate),
            ) from error
        candidate_hash = hashlib.sha256(content).hexdigest()

        operation_request = request.request_dict(
            path=str(target),
            previousSha256=expected,
            sha256=candidate_hash,
            contentFile=str(candidate),
        )
        with resource_lock(request.resource, self.store.home):
            cached = self.store.begin_operation(
                request,
                "replace-file",
                operation_request,
                lock_held=True,
            )
            if cached is not None:
                if not bool(cached.get("ok", False)):
                    raise LeaseError(
                        str(cached.get("error", "replace-file-failed")),
                        code=3,
                        operationId=request.operation_id,
                    )
                return cached

            # Recheck after recording intent. A concurrent external writer must
            # not turn an expected-hash operation into an unguarded overwrite.
            if target.is_symlink():
                raise LeaseError("target-is-symlink", code=64, path=str(target))
            current_hash = file_sha256(target)
            if current_hash != expected:
                failure: dict[str, object] = {
                    "ok": False,
                    "operation": "replace-file",
                    "operationId": request.operation_id,
                    "idempotent": False,
                    "error": "file-version-conflict",
                    "path": str(target),
                    "previousSha256": expected,
                    "actualSha256": current_hash,
                }
                self.store.complete_operation(
                    request,
                    "replace-file",
                    operation_request,
                    failure,
                    lock_held=True,
                )
                raise LeaseError(
                    "file-version-conflict",
                    code=3,
                    path=str(target),
                    expectedSha256=expected,
                    actualSha256=current_hash,
                )

            self.store.validate_current(request, lock_held=True)
            mode = stat.S_IMODE(target.stat().st_mode)
            atomic_replace(target, content, mode)
            receipt: dict[str, object] = {
                "ok": True,
                "operation": "replace-file",
                "operationId": request.operation_id,
                "idempotent": False,
                "path": str(target),
                "previousSha256": expected,
                "sha256": candidate_hash,
                "recovered": False,
                "ttl": float(request.ttl),
            }
            return self.store.complete_operation(
                request,
                "replace-file",
                operation_request,
                receipt,
                lock_held=True,
            )


def replace_file(
    store: LeaseStore,
    request: MutationRequest,
    path: str | os.PathLike[str],
    expected_sha256: str,
    content_file: str | os.PathLike[str],
) -> dict[str, object]:
    """Convenience wrapper around :class:`FileReplacer`."""

    require_text(request.operation_id, "operation-id")
    return FileReplacer(store).replace(request, path, expected_sha256, content_file)
