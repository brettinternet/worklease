"""Supported public API for provider-neutral same-host work leases.

The names exported here are the compatibility surface for library callers.
Persistence, locking, serialization, and provider implementation modules
remain private implementation details.
"""

from typing import TYPE_CHECKING

from .adapters import ProviderAdapter, ResourceKey, key_result
from .instructions import agent_instructions
from .models import (
    DEFAULT_TTL,
    MAX_BUNDLE_RESOURCES,
    MAX_CHECKPOINT_BYTES,
    MAX_TTL,
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
    require_bundle_resources,
    require_ttl,
    serialize_checkpoint,
)
from .store import LeaseStore

if TYPE_CHECKING:
    from .execution import GuardedExecutor, execute, execute_bundle
    from .lease_file import (
        LeaseFileState,
        check_lease_file_writable,
        clear_lease_file,
        read_lease_file,
        write_lease_file,
    )
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
    if name in {
        "LeaseFileState",
        "check_lease_file_writable",
        "clear_lease_file",
        "read_lease_file",
        "write_lease_file",
    }:
        from .lease_file import (
            LeaseFileState,
            check_lease_file_writable,
            clear_lease_file,
            read_lease_file,
            write_lease_file,
        )

        return {
            "LeaseFileState": LeaseFileState,
            "check_lease_file_writable": check_lease_file_writable,
            "clear_lease_file": clear_lease_file,
            "read_lease_file": read_lease_file,
            "write_lease_file": write_lease_file,
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
    "DEFAULT_TTL",
    "FileReplacer",
    "GuardedExecutor",
    "LeaseError",
    "LeaseFileState",
    "LeaseStore",
    "MAX_BUNDLE_RESOURCES",
    "MAX_CHECKPOINT_BYTES",
    "MAX_TTL",
    "MutationRequest",
    "ProviderAdapter",
    "ResourceKey",
    "TransferRequest",
    "agent_instructions",
    "check_lease_file_writable",
    "clear_lease_file",
    "execute",
    "execute_bundle",
    "key_result",
    "read_lease_file",
    "replace_file",
    "require_bundle_resources",
    "require_ttl",
    "serialize_checkpoint",
    "write_lease_file",
]
