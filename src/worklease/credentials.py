"""Resolve claim bearer credentials without exposing secret values."""

from __future__ import annotations

import os
import stat
from collections.abc import Callable
from typing import Any

from .models import LeaseError

MAX_CREDENTIAL_BYTES = 4096


def _error(reason: str) -> LeaseError:
    """Build a redacted, parser-style credential error."""

    return LeaseError(reason, code=64)


def _validate(value: str, *, source: str) -> str:
    """Validate one decoded credential and return it unchanged."""

    if not isinstance(value, str):
        raise _error(f"credential-{source}-malformed")
    if not value or "\x00" in value:
        raise _error(f"credential-{source}-malformed")
    if value.endswith("\n"):
        value = value[:-1]
    if not value or "\n" in value or "\r" in value:
        raise _error(f"credential-{source}-malformed")
    if len(value.encode("utf-8")) > MAX_CREDENTIAL_BYTES:
        raise _error(f"credential-{source}-oversized")
    return value


def _decode(raw: bytes, *, source: str) -> str:
    if len(raw) > MAX_CREDENTIAL_BYTES:
        raise _error(f"credential-{source}-oversized")
    try:
        value = raw.decode("utf-8")
    except UnicodeDecodeError as error:
        raise _error(f"credential-{source}-malformed") from error
    return _validate(value, source=source)


def _read_fd(fd: int, *, source: str) -> str:
    """Read a descriptor to EOF, retaining no more than the size limit."""

    chunks: list[bytes] = []
    size = 0
    try:
        while True:
            chunk = os.read(fd, MAX_CREDENTIAL_BYTES + 1 - size)
            if not chunk:
                break
            chunks.append(chunk)
            size += len(chunk)
            if size > MAX_CREDENTIAL_BYTES:
                raise _error(f"credential-{source}-oversized")
    except LeaseError:
        raise
    except OSError as error:
        raise _error(f"credential-{source}-unreadable") from error
    return _decode(b"".join(chunks), source=source)


def _read_file(path: Any) -> str:
    """Read an owner-only regular file without following symlinks."""

    try:
        flags = os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW
        fd = os.open(os.fspath(path), flags)
    except (OSError, TypeError, ValueError) as error:
        raise _error("credential-file-unreadable") from error
    try:
        try:
            metadata = os.fstat(fd)
        except OSError as error:
            raise _error("credential-file-unreadable") from error
        if (
            not stat.S_ISREG(metadata.st_mode)
            or metadata.st_uid != os.geteuid()
            or metadata.st_mode & 0o077
        ):
            raise _error("credential-file-unsafe")
        return _read_fd(fd, source="file")
    finally:
        os.close(fd)


def _read_descriptor(value: Any) -> str:
    """Duplicate and read an inherited descriptor without leaking it to children."""

    try:
        fd = int(value)
    except (TypeError, ValueError, OverflowError) as error:
        raise _error("credential-fd-malformed") from error
    if fd < 0:
        raise _error("credential-fd-malformed")
    try:
        duplicate = os.dup(fd)
    except OSError as error:
        raise _error("credential-fd-unreadable") from error
    try:
        os.set_inheritable(duplicate, False)
        return _read_fd(duplicate, source="fd")
    finally:
        os.close(duplicate)


def resolve_credential(
    *, token: Any = None, token_file: Any = None, token_fd: Any = None
) -> str:
    """Resolve exactly one claim credential source.

    Direct ``token`` remains supported for compatibility, while file and file
    descriptor sources keep the secret out of process arguments and history.
    """

    sources: tuple[tuple[str, Any, Callable[[Any], str]], ...] = (
        ("argv", token, lambda value: _validate(value, source="argv")),
        ("file", token_file, lambda value: _read_file(value)),
        ("fd", token_fd, lambda value: _read_descriptor(value)),
    )
    provided = tuple(source for source, value, _ in sources if value is not None)
    if len(provided) != 1:
        raise _error("credential-source-conflict" if provided else "credential-missing")
    source, value, reader = next(item for item in sources if item[0] == provided[0])
    try:
        return reader(value)
    except LeaseError:
        raise
    except Exception as error:
        raise _error(f"credential-{source}-unreadable") from error
