"""Provider-owned source workflow protocol for external adapters."""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from typing import Protocol, runtime_checkable

from .models import (
    CONTRACT_VERSION,
    DiscoverResult,
    MutationResult,
    ReadResult,
    ResolveResult,
    ResourcePolicyResult,
    ReviewResult,
    Source,
    WorkRef,
)


@runtime_checkable
class SourceProvider(Protocol):
    """Typed provider boundary; scheduling and lease lifecycle stay external."""

    kind: str
    contract_version: int

    def resolve(
        self,
        arguments: Sequence[str],
        context: Mapping[str, object],
    ) -> ResolveResult:
        """Resolve caller arguments into ordered sources or a capability result."""
        ...

    def discover(self, source: Source, selector: str | None = None) -> DiscoverResult:
        """Discover a complete source collection and dependency closure."""
        ...

    def read_item(self, ref: WorkRef) -> ReadResult:
        """Read one authoritative source-qualified item."""
        ...

    def resource_policy(
        self,
        ref: WorkRef,
        work_key: str,
    ) -> ResourcePolicyResult:
        """Select a local resource identity and guarantee declaration."""
        ...

    def write_state(
        self,
        ref: WorkRef,
        patch: Mapping[str, object],
        authority: object,
        expected_version: str | None = None,
    ) -> MutationResult:
        """Apply one authorized provider state patch."""
        ...

    def record_progress(
        self,
        ref: WorkRef,
        checkpoint: Mapping[str, object],
        authority: object,
        expected_version: str | None = None,
    ) -> MutationResult:
        """Persist one authorized implementation/review checkpoint."""
        ...

    def resolve_review_boundary(
        self,
        source: Source,
        explicit_selector: str | None,
        authority: object,
    ) -> ReviewResult:
        """Resolve one provider-authorized exact review target."""
        ...

    def archive(
        self,
        target: Source | WorkRef,
        authority: object,
        expected_version: str | None = None,
    ) -> MutationResult:
        """Archive one explicitly authorized source or item."""
        ...


__all__ = ["CONTRACT_VERSION", "SourceProvider"]
