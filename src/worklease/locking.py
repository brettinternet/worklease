"""Non-blocking same-host file locks keyed by opaque resources."""

from __future__ import annotations

import fcntl
import hashlib
import os
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

from .models import LeaseError, require_resource
from .sqlite import lease_home


def resource_lock_path(
    resource: str, home: str | os.PathLike[str] | None = None
) -> Path:
    """Hash only for a safe filename; the resource itself remains opaque."""

    require_resource(resource)
    directory = lease_home(home) / "locks"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    digest = hashlib.sha256(resource.encode("utf-8")).hexdigest()
    return directory / f"{digest}.lock"


@contextmanager
def resource_lock(
    resource: str, home: str | os.PathLike[str] | None = None
) -> Iterator[None]:
    """Hold one non-blocking exclusive POSIX lock for a resource."""

    path = resource_lock_path(resource, home)
    descriptor = os.open(path, os.O_CREAT | os.O_RDWR, 0o600)
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise LeaseError("resource-guarded", resource=resource) from error
        yield
    finally:
        os.close(descriptor)
