"""Supported public API for provider-neutral same-host work leases.

The names exported here are the compatibility surface for library callers.
Persistence, locking, serialization, and provider implementation modules
remain private implementation details.
"""

from importlib.metadata import version

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
)
from .replacement import FileReplacer, replace_file
from .store import LeaseStore

__version__ = version("worklease")

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
]
