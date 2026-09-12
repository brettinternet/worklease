"""Resolve contextual lease handles by SHA-256 of the UTF-8 context root."""

from __future__ import annotations

import fcntl
import hashlib
import os
import stat
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

from .execution_context import _git_output
from .models import LeaseError
from .sqlite import lease_home, secure_directory

_CONTEXT_LEASE_DIRECTORY = "context-leases"


def _caller_directory(cwd: str | os.PathLike[str] | None) -> Path:
    try:
        path = (
            (Path.cwd() if cwd is None else Path(cwd)).expanduser().resolve(strict=True)
        )
    except (OSError, RuntimeError, TypeError, ValueError) as error:
        raise LeaseError("lease-context-directory-invalid", code=64) from error
    if not path.is_dir():
        raise LeaseError("lease-context-directory-invalid", code=64)
    return path


def resolve_context_root(cwd: str | os.PathLike[str] | None = None) -> Path:
    """Return the resolved Git worktree root or the resolved caller directory."""

    caller = _caller_directory(cwd)
    if _git_output(caller, "rev-parse", "--is-inside-work-tree") != "true":
        return caller
    top_level = _git_output(caller, "rev-parse", "--show-toplevel")
    if top_level is None:
        return caller
    try:
        root = Path(top_level).resolve(strict=True)
    except OSError, RuntimeError:
        return caller
    return root if root.is_dir() else caller


def context_lease_path(
    home: str | os.PathLike[str] | None = None,
    *,
    cwd: str | os.PathLike[str] | None = None,
) -> Path:
    """Return the contextual handle path without creating state on disk."""

    root = resolve_context_root(cwd)
    try:
        encoded_root = str(root).encode("utf-8")
    except UnicodeEncodeError as error:
        raise LeaseError("lease-context-directory-invalid", code=64) from error
    context_id = hashlib.sha256(encoded_root).hexdigest()
    return lease_home(home) / _CONTEXT_LEASE_DIRECTORY / f"{context_id}.lease"


def _validate_context_directory(path: Path) -> None:
    try:
        metadata = path.lstat()
    except FileNotFoundError:
        return
    except OSError as error:
        raise LeaseError("lease-context-directory-unsafe", code=64) from error
    if (
        stat.S_ISLNK(metadata.st_mode)
        or not stat.S_ISDIR(metadata.st_mode)
        or metadata.st_uid != os.geteuid()
    ):
        raise LeaseError("lease-context-directory-unsafe", code=64)


@contextmanager
def context_lease_lock(
    home: str | os.PathLike[str] | None = None,
    *,
    cwd: str | os.PathLike[str] | None = None,
    create: bool = False,
) -> Iterator[None]:
    """Serialize contextual handle validation, store dispatch, and persistence.

    The lock lives beside the contextual handles and is deliberately process
    scoped via ``flock`` so independent CLI processes cannot both validate an
    old handle before either one persists its replacement.
    """

    directory = context_lease_path(home, cwd=cwd).parent
    if create:
        prepare_context_lease_path(home, cwd=cwd)
    elif not directory.is_dir():
        yield
        return
    try:
        # Revalidate the directory immediately before opening the lock. This
        # keeps contextual reads from proceeding through a replaced or
        # permission-relaxed state directory.
        secure_directory(directory)
        flags = os.O_RDWR | os.O_CREAT | os.O_CLOEXEC | getattr(os, "O_NOFOLLOW", 0)
        descriptor = os.open(directory / ".lock", flags, 0o600)
        os.fchmod(descriptor, 0o600)
    except OSError as error:
        raise LeaseError("lease-context-directory-unsafe", code=64) from error
    try:
        fcntl.flock(descriptor, fcntl.LOCK_EX)
        yield
    finally:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_UN)
        finally:
            os.close(descriptor)


def prepare_context_lease_path(
    home: str | os.PathLike[str] | None = None,
    *,
    cwd: str | os.PathLike[str] | None = None,
) -> Path:
    """Create and validate the private directory for a contextual handle."""

    path = context_lease_path(home, cwd=cwd)
    _validate_context_directory(path.parent)
    try:
        secure_directory(path.parent)
    except OSError as error:
        raise LeaseError("lease-context-directory-unsafe", code=64) from error
    _validate_context_directory(path.parent)
    return path
