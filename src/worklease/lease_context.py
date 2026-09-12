"""Resolve contextual lease handles by SHA-256 of the UTF-8 context root."""

from __future__ import annotations

import hashlib
import os
import stat
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
    context_id = hashlib.sha256(str(root).encode("utf-8")).hexdigest()
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
