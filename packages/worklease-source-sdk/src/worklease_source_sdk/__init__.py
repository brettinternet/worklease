"""Public version-one source-provider SDK facade."""

from .conformance import (
    ProviderConformanceCase,
    ProviderConformanceReport,
    assert_provider_conformance,
    run_provider_conformance,
)
from .models import (
    CONTRACT_VERSION,
    CapabilityResult,
    DiscoverResult,
    MutationResult,
    ProviderReceipt,
    ReadResult,
    ReceiptOutcome,
    ResolveResult,
    ResourcePolicyResult,
    ResourcePolicySelection,
    ReviewBoundary,
    ReviewResult,
    Source,
    WorkItem,
    WorkItemState,
    WorkRef,
)
from .provider import SourceProvider

__version__ = "0.7.0"

__all__ = [
    "CONTRACT_VERSION",
    "CapabilityResult",
    "DiscoverResult",
    "MutationResult",
    "ProviderReceipt",
    "ProviderConformanceCase",
    "ProviderConformanceReport",
    "assert_provider_conformance",
    "run_provider_conformance",
    "ReadResult",
    "ReceiptOutcome",
    "ResourcePolicyResult",
    "ResourcePolicySelection",
    "ResolveResult",
    "ReviewBoundary",
    "ReviewResult",
    "Source",
    "SourceProvider",
    "WorkItem",
    "WorkItemState",
    "WorkRef",
    "__version__",
]
