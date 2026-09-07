"""Supported public API for provider-neutral same-host work leases.

The names exported here are the compatibility surface for library callers.
Persistence, locking, serialization, and provider implementation modules
remain private implementation details.
"""

from typing import TYPE_CHECKING

from .adapters import ProviderAdapter, ResourceKey
from .models import (
    AcquireRequest,
    BundleAcquireRequest,
    BundleClaim,
    BundleMutationRequest,
    BundleStatusRequest,
    Claim,
    ClaimError,
    LeaseError,
    MutationRequest,
    TransferRequest,
)
from .store import LeaseStore

if TYPE_CHECKING:
    from .execution import GuardedExecutor, execute, execute_bundle
    from .replacement import FileReplacer, replace_file

__version__: str


def __getattr__(name: str) -> object:
    """Resolve version and heavyweight execution APIs only when requested."""

    if name == "__version__":
        from importlib.metadata import version

        return version("worklease")
    if name in {"GuardedExecutor", "execute", "execute_bundle"}:
        from .execution import GuardedExecutor, execute, execute_bundle

        return {
            "GuardedExecutor": GuardedExecutor,
            "execute": execute,
            "execute_bundle": execute_bundle,
        }[name]
    if name in {"FileReplacer", "replace_file"}:
        from .replacement import FileReplacer, replace_file

        return {"FileReplacer": FileReplacer, "replace_file": replace_file}[name]
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


__all__ = [
    "__version__",
    "AcquireRequest",
    "BundleAcquireRequest",
    "BundleClaim",
    "BundleMutationRequest",
    "BundleStatusRequest",
    "Claim",
    "ClaimError",
    "FileReplacer",
    "GuardedExecutor",
    "LeaseError",
    "LeaseStore",
    "MutationRequest",
    "ProviderAdapter",
    "ResourceKey",
    "execute",
    "execute_bundle",
    "replace_file",
    "TransferRequest",
]
