"""Non-blocking same-host file locks keyed by opaque resources."""

from __future__ import annotations

import fcntl
import hashlib
import os
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

from .models import LeaseError, require_resource
from .sqlite import lease_home, open_private_file, secure_directory


def resource_lock_path(
    resource: str, home: str | os.PathLike[str] | None = None
) -> Path:
    """Hash only for a safe filename; the resource itself remains opaque."""

    require_resource(resource)
    directory = secure_directory(lease_home(home) / "locks")
    digest = hashlib.sha256(resource.encode("utf-8")).hexdigest()
    return directory / f"{digest}.lock"


@contextmanager
def resource_lock(
    resource: str, home: str | os.PathLike[str] | None = None
) -> Iterator[None]:
    """Hold one non-blocking exclusive POSIX lock for a resource."""

    path = resource_lock_path(resource, home)
    descriptor = open_private_file(path)
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise LeaseError("resource-guarded", resource=resource) from error
        yield
    finally:
        os.close(descriptor)
