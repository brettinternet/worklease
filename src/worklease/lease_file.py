"""Versioned, owner-only lease handle files for CLI callers."""

from __future__ import annotations

import json
import os
import stat
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, NoReturn

from .models import LeaseError, require_bundle_resources, require_resource, require_text

LEASE_FILE_SCHEMA_VERSION = 1
MAX_LEASE_FILE_BYTES = 64 * 1024
_GUARANTEES = frozenset({"fenced", "local-coordination"})


def _error(reason: str) -> LeaseError:
    """Build a non-secret lease-file validation error."""

    return LeaseError(reason, code=64)


@dataclass(frozen=True, slots=True)
class LeaseFileState:
    """The caller-owned fields persisted in a lease handle."""

    resource: str | None
    resources: tuple[str, ...] | None
    claim_id: str
    token: str
    revision: int
    expires_at: str
    guarantee: str

    def __post_init__(self) -> None:
        if (self.resource is None) == (self.resources is None):
            raise _error("lease-file-kind-mismatch")
        if self.resource is not None:
            require_resource(self.resource)
        else:
            assert self.resources is not None
            require_bundle_resources(self.resources)
        require_text(self.claim_id, "claim-id")
        require_text(self.token, "token")
        if isinstance(self.revision, bool) or not isinstance(self.revision, int):
            raise _error("lease-file-invalid-revision")
        if self.revision < 1:
            raise _error("lease-file-invalid-revision")
        require_text(self.expires_at, "expires-at")
        if self.guarantee not in _GUARANTEES:
            raise _error("lease-file-invalid-guarantee")

    @property
    def is_bundle(self) -> bool:
        return self.resources is not None

    def to_dict(self) -> dict[str, Any]:
        """Return the strict version-one on-disk representation."""

        value: dict[str, Any] = {
            "schemaVersion": LEASE_FILE_SCHEMA_VERSION,
            "claimId": self.claim_id,
            "token": self.token,
            "revision": self.revision,
            "expiresAt": self.expires_at,
            "guarantee": self.guarantee,
        }
        if self.resource is not None:
            value["resource"] = self.resource
        else:
            assert self.resources is not None
            value["resources"] = list(self.resources)
        return value

    @classmethod
    def from_claim(
        cls,
        claim: dict[str, Any],
        *,
        prior: LeaseFileState | None = None,
        token: str | None = None,
    ) -> LeaseFileState:
        """Build handle state from a claim receipt and optional redacted state."""

        resource_value = claim.get("resource")
        resources_value = claim.get("resources")
        resource: str | None
        resources: tuple[str, ...] | None
        if resource_value is not None:
            if resources_value is not None:
                raise _error("lease-file-kind-mismatch")
            if not isinstance(resource_value, str):
                raise _error("lease-file-invalid-resource")
            resource = resource_value
            resources = None
        elif resources_value is not None:
            try:
                resources = require_bundle_resources(resources_value)
            except LeaseError as error:
                raise _error("lease-file-invalid-resources") from error
            resource = None
        else:
            if prior is None:
                raise _error("lease-file-claim-missing-resource")
            resource = prior.resource
            resources = prior.resources

        claim_id = claim.get("claimId", prior.claim_id if prior else None)
        revision = claim.get("revision", prior.revision if prior else None)
        expires_at = claim.get("expiresAt", prior.expires_at if prior else None)
        guarantee = claim.get("guarantee", prior.guarantee if prior else None)
        actual_token = claim.get("token", token)
        if actual_token is None and prior is not None:
            actual_token = prior.token
        if not isinstance(claim_id, str):
            raise _error("lease-file-claim-missing-claim-id")
        if not isinstance(revision, int) or isinstance(revision, bool):
            raise _error("lease-file-claim-missing-revision")
        if not isinstance(expires_at, str):
            raise _error("lease-file-claim-missing-expiry")
        if not isinstance(guarantee, str):
            raise _error("lease-file-claim-missing-guarantee")
        if not isinstance(actual_token, str):
            raise _error("lease-file-claim-missing-token")
        return cls(
            resource=resource,
            resources=resources,
            claim_id=claim_id,
            token=actual_token,
            revision=revision,
            expires_at=expires_at,
            guarantee=guarantee,
        )


def _path(value: str | os.PathLike[str]) -> Path:
    try:
        return Path(value).expanduser()
    except (TypeError, ValueError, OSError) as error:
        raise _error("lease-file-unreadable") from error


def _check_existing(path: Path, *, require_private: bool) -> os.stat_result | None:
    """Reject unsafe existing paths without following a symlink."""

    try:
        metadata = path.lstat()
    except FileNotFoundError:
        return None
    except OSError as error:
        raise _error("lease-file-unreadable") from error
    if stat.S_ISLNK(metadata.st_mode):
        raise _error("lease-file-is-symlink")
    if not stat.S_ISREG(metadata.st_mode):
        raise _error("lease-file-unsafe")
    if metadata.st_uid != os.geteuid():
        raise _error("lease-file-unsafe")
    if require_private and metadata.st_mode & 0o077:
        raise _error("lease-file-unsafe")
    return metadata


def _read_bytes(path: Path) -> bytes:
    """Read one owner-only regular file without following symlinks."""

    try:
        flags = os.O_RDONLY | os.O_CLOEXEC | getattr(os, "O_NOFOLLOW", 0)
        descriptor = os.open(os.fspath(path), flags)
    except FileNotFoundError as error:
        raise _error("lease-file-not-found") from error
    except (OSError, TypeError, ValueError) as error:
        raise _error("lease-file-unreadable") from error
    try:
        metadata = os.fstat(descriptor)
        if (
            not stat.S_ISREG(metadata.st_mode)
            or metadata.st_uid != os.geteuid()
            or metadata.st_mode & 0o077
        ):
            raise _error("lease-file-unsafe")
        chunks: list[bytes] = []
        size = 0
        while True:
            chunk = os.read(descriptor, min(16 * 1024, MAX_LEASE_FILE_BYTES + 1 - size))
            if not chunk:
                break
            chunks.append(chunk)
            size += len(chunk)
            if size > MAX_LEASE_FILE_BYTES:
                raise _error("lease-file-too-large")
        return b"".join(chunks)
    except LeaseError:
        raise
    except OSError as error:
        raise _error("lease-file-unreadable") from error
    finally:
        os.close(descriptor)


def _reject_json_constant(value: str) -> NoReturn:
    raise ValueError(value)


def _parse(raw: bytes) -> LeaseFileState:
    try:
        text = raw.decode("utf-8")
        value = json.loads(text, parse_constant=_reject_json_constant)
    except (UnicodeDecodeError, TypeError, ValueError) as error:
        raise _error("lease-file-malformed") from error
    if not isinstance(value, dict):
        raise _error("lease-file-malformed")
    if value.get("schemaVersion") != LEASE_FILE_SCHEMA_VERSION:
        raise _error("lease-file-unsupported-version")
    allowed = {
        "schemaVersion",
        "resource",
        "resources",
        "claimId",
        "token",
        "revision",
        "expiresAt",
        "guarantee",
    }
    if set(value) - allowed:
        raise _error("lease-file-malformed")
    try:
        has_resource = "resource" in value
        has_resources = "resources" in value
        if has_resource == has_resources:
            raise _error("lease-file-kind-mismatch")
        resource = value.get("resource") if has_resource else None
        resources = (
            require_bundle_resources(value.get("resources")) if has_resources else None
        )
        if has_resource:
            if not isinstance(resource, str):
                raise _error("lease-file-malformed")
            require_resource(resource)
        claim_id = value["claimId"]
        token = value["token"]
        revision = value["revision"]
        expires_at = value["expiresAt"]
        guarantee = value["guarantee"]
    except KeyError as error:
        raise _error("lease-file-malformed") from error
    except LeaseError as error:
        raise _error("lease-file-malformed") from error
    if not isinstance(resource, (str, type(None))):
        raise _error("lease-file-malformed")
    if not isinstance(claim_id, str) or not isinstance(token, str):
        raise _error("lease-file-malformed")
    if not isinstance(revision, int) or isinstance(revision, bool):
        raise _error("lease-file-malformed")
    if not isinstance(expires_at, str) or not isinstance(guarantee, str):
        raise _error("lease-file-malformed")
    try:
        return LeaseFileState(
            resource=resource,
            resources=resources,
            claim_id=claim_id,
            token=token,
            revision=revision,
            expires_at=expires_at,
            guarantee=guarantee,
        )
    except LeaseError as error:
        raise _error("lease-file-malformed") from error


def read_lease_file(path: str | os.PathLike[str]) -> LeaseFileState:
    """Read and validate one private version-one lease handle."""

    lease_path = _path(path)
    _check_existing(lease_path, require_private=True)
    return _parse(_read_bytes(lease_path))


def check_lease_file_writable(path: str | os.PathLike[str]) -> None:
    """Reject a destination that write_lease_file could not replace.

    Called before a mutation commits, because a handle write that fails
    afterwards has no way to return the claim's only copy of its token.
    """

    lease_path = _path(path)
    _check_existing(lease_path, require_private=False)
    parent = lease_path.parent
    if not parent.is_dir() or not os.access(parent, os.W_OK | os.X_OK):
        raise _error("lease-file-unwritable")


def write_lease_file(path: str | os.PathLike[str], state: LeaseFileState) -> None:
    """Atomically write one mode-0600 lease handle and fsync its directory."""

    lease_path = _path(path)
    _check_existing(lease_path, require_private=False)
    encoded = (
        json.dumps(state.to_dict(), sort_keys=True, separators=(",", ":")) + "\n"
    ).encode("utf-8")
    parent = lease_path.parent
    descriptor, temporary_name = tempfile.mkstemp(
        prefix=f".{lease_path.name}.worklease-", dir=parent
    )
    temporary = Path(temporary_name)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "wb") as target:
            target.write(encoded)
            target.flush()
            os.fsync(target.fileno())
        os.replace(temporary, lease_path)
        directory = os.open(parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if temporary.exists():
            temporary.unlink()


def clear_lease_file(path: str | os.PathLike[str]) -> None:
    """Remove a lease handle after its claim has been released."""

    lease_path = _path(path)
    existing = _check_existing(lease_path, require_private=False)
    if existing is not None:
        try:
            os.unlink(lease_path)
        except OSError as error:
            raise _error("lease-file-unreadable") from error
        directory = os.open(lease_path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
