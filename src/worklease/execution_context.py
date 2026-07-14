"""Validated working-directory selection for guarded provider execution."""

from __future__ import annotations

import os
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Literal

from .models import LeaseError

ExecutionDirectoryMode = Literal["caller", "provider-directory", "git-primary"]
_GIT_ROUTING_VARIABLES = frozenset(
    {
        "GIT_DIR",
        "GIT_WORK_TREE",
        "GIT_COMMON_DIR",
        "GIT_INDEX_FILE",
        "GIT_OBJECT_DIRECTORY",
        "GIT_ALTERNATE_OBJECT_DIRECTORIES",
        "GIT_GRAFT_FILE",
        "GIT_NAMESPACE",
        "GIT_CEILING_DIRECTORIES",
        "GIT_DISCOVERY_ACROSS_FILESYSTEM",
    }
)


@dataclass(frozen=True, slots=True)
class ExecutionDirectory:
    mode: ExecutionDirectoryMode
    path: Path | None = None

    def __post_init__(self) -> None:
        if self.mode == "caller":
            if self.path is not None:
                raise LeaseError("invalid-execution-directory", code=64)
        elif self.path is None or not self.path.is_absolute():
            raise LeaseError("invalid-execution-directory", code=64)

    @property
    def provider(self) -> bool:
        return self.mode != "caller"

    def request_value(self) -> dict[str, str]:
        value = {"mode": self.mode}
        if self.path is not None:
            value["path"] = str(self.path)
        return value


def _git_output(cwd: Path, *arguments: str) -> str | None:
    environment = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
    try:
        completed = subprocess.run(
            ["git", "-C", str(cwd), *arguments],
            capture_output=True,
            check=False,
            env=environment,
            text=True,
        )
    except OSError:
        return None
    if completed.returncode != 0:
        return None
    return completed.stdout.strip() or None


def _resolve_directory(value: str | os.PathLike[str], *, reason: str) -> Path:
    try:
        path = Path(value).expanduser().resolve(strict=True)
    except (OSError, RuntimeError, TypeError) as error:
        raise LeaseError(reason, code=64) from error
    if not path.is_dir():
        raise LeaseError(reason, code=64)
    return path


def _primary_worktree(probe: Path, common: Path) -> Path | None:
    candidates = [probe]
    try:
        candidates.extend(
            candidate for candidate in common.parent.iterdir() if candidate.is_dir()
        )
    except OSError:
        return None
    for candidate in candidates:
        marker = candidate / ".git"
        try:
            if marker.is_dir() and marker.resolve(strict=True) == common:
                return candidate.resolve(strict=True)
            if marker.is_file():
                value = marker.read_text(encoding="utf-8").strip()
                if value.startswith("gitdir:"):
                    target = Path(value[7:].strip())
                    if not target.is_absolute():
                        target = candidate / target
                    if target.resolve(strict=True) == common:
                        return candidate.resolve(strict=True)
        except OSError, RuntimeError, UnicodeError:
            continue
    return None


def _worktree_paths(probe: Path, common: Path | None = None) -> list[Path]:
    if common is None:
        value = _git_output(probe, "rev-parse", "--git-common-dir")
        if value is not None:
            common = Path(value)
            if not common.is_absolute():
                common = probe / common
            try:
                common = common.resolve(strict=True)
            except OSError, RuntimeError:
                common = None
    environment = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
    try:
        completed = subprocess.run(
            ["git", "-C", str(probe), "worktree", "list", "--porcelain"],
            capture_output=True,
            check=False,
            env=environment,
            text=True,
        )
    except OSError as error:
        raise LeaseError("git-primary-unavailable", code=64) from error
    if completed.returncode != 0:
        raise LeaseError("git-primary-unavailable", code=64)
    paths: list[Path] = []
    for line in completed.stdout.splitlines():
        if line.startswith("worktree "):
            try:
                path = _resolve_directory(
                    line.removeprefix("worktree "),
                    reason="invalid-provider-working-directory",
                )
                if common is not None and path == common:
                    path = _primary_worktree(probe, common) or path
                paths.append(path)
            except LeaseError:
                # A prunable linked worktree is not a candidate; continue
                # resolving the registered primary checkout.
                continue
        elif line == "bare":
            raise LeaseError("bare-repository", code=64)
    return paths


def _git_primary(cwd: Path) -> Path:
    probe = _resolve_directory(cwd, reason="invalid-provider-working-directory")
    if _git_output(probe, "rev-parse", "--is-bare-repository") == "true":
        raise LeaseError("bare-repository", code=64)
    if (
        _git_output(probe, "rev-parse", "--show-toplevel") is None
        or _git_output(probe, "rev-parse", "--git-common-dir") is None
    ):
        raise LeaseError("not-git-repository", code=64)
    common = Path(_git_output(probe, "rev-parse", "--git-common-dir") or "")
    if not common.is_absolute():
        common = probe / common
    try:
        common = common.resolve(strict=True)
    except (OSError, RuntimeError) as error:
        raise LeaseError("git-primary-unavailable", code=64) from error
    candidates: list[Path] = []
    for candidate in _worktree_paths(probe, common):
        git_dir = _git_output(candidate, "rev-parse", "--git-dir")
        if git_dir is None:
            continue
        path = Path(git_dir)
        if not path.is_absolute():
            path = candidate / path
        try:
            if path.resolve(strict=True) == common:
                candidates.append(candidate)
        except OSError, RuntimeError:
            continue
    if len(candidates) == 0:
        raise LeaseError("unregistered-primary-worktree", code=64)
    if len(candidates) > 1:
        raise LeaseError("ambiguous-primary-worktree", code=64)
    return candidates[0]


def resolve_execution_directory(
    provider_directory: str | os.PathLike[str] | None = None,
    *,
    git_primary: bool = False,
    cwd: str | os.PathLike[str] | None = None,
) -> ExecutionDirectory:
    """Resolve a directory without changing generic exec semantics."""
    if provider_directory is not None and git_primary:
        raise LeaseError("conflicting-execution-directory", code=64)
    if provider_directory is None and not git_primary:
        return ExecutionDirectory("caller")
    if provider_directory is not None:
        return ExecutionDirectory(
            "provider-directory",
            _resolve_directory(
                provider_directory, reason="invalid-provider-working-directory"
            ),
        )
    return ExecutionDirectory(
        "git-primary", _git_primary(Path.cwd() if cwd is None else Path(cwd))
    )


def provider_environment() -> dict[str, str]:
    """Remove only variables that redirect Git repository execution."""
    return {k: v for k, v in os.environ.items() if k not in _GIT_ROUTING_VARIABLES}
