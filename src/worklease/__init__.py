"""Supported public API for provider-neutral same-host work leases.

The names exported here are the compatibility surface for library callers.
Persistence, locking, serialization, and provider implementation modules
remain private implementation details.
"""

from .adapters import ProviderAdapter, ResourceKey
from .execution import GuardedExecutor, execute, execute_bundle
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
from .replacement import FileReplacer, replace_file
from .store import LeaseStore

__version__: str


def __getattr__(name: str) -> str:
    """Resolve the package version lazily; importlib.metadata is slow to import."""

    if name == "__version__":
        from importlib.metadata import version

        return version("worklease")
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
