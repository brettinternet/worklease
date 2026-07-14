"""Immutable models for the version-one source-provider contract."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass
from types import MappingProxyType
from typing import Literal

CONTRACT_VERSION = 1
type ReceiptOutcome = Literal["confirmed", "ambiguous"]


def _text(value: str, field: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{field} must be a non-empty string")
    return value.strip()


def _mapping(value: Mapping[str, object] | None) -> Mapping[str, object]:
    if value is None:
        return MappingProxyType({})
    return MappingProxyType(dict(value))


@dataclass(frozen=True, slots=True)
class Source:
    """A caller-qualified provider collection."""

    id: str
    kind: str
    locator: str
    name: str
    provider_data: Mapping[str, object] | None = None

    def __post_init__(self) -> None:
        for field in ("id", "kind", "locator", "name"):
            object.__setattr__(self, field, _text(getattr(self, field), field))
        object.__setattr__(self, "provider_data", _mapping(self.provider_data))


@dataclass(frozen=True, slots=True)
class WorkRef:
    """A source-qualified opaque work-item reference."""

    source_id: str
    item_id: str

    def __post_init__(self) -> None:
        object.__setattr__(self, "source_id", _text(self.source_id, "source_id"))
        object.__setattr__(self, "item_id", _text(self.item_id, "item_id"))


@dataclass(frozen=True, slots=True)
class WorkItemState:
    """Normalized state while preserving provider-specific status text."""

    status: str
    is_terminal: bool
    is_blocked: bool
    blocker: str | None = None

    def __post_init__(self) -> None:
        object.__setattr__(self, "status", _text(self.status, "status"))
        if self.blocker is not None:
            object.__setattr__(self, "blocker", _text(self.blocker, "blocker"))


@dataclass(frozen=True, slots=True)
class WorkItem:
    """A discovered item and its complete source-qualified dependencies."""

    ref: WorkRef
    title: str
    body: str
    dependencies: tuple[WorkRef, ...]
    state: WorkItemState
    provider_version: str | None = None
    provider_data: Mapping[str, object] | None = None

    def __post_init__(self) -> None:
        object.__setattr__(self, "title", _text(self.title, "title"))
        if not isinstance(self.body, str):
            raise TypeError("body must be a string")
        if not isinstance(self.dependencies, tuple):
            object.__setattr__(self, "dependencies", tuple(self.dependencies))
        if not all(isinstance(value, WorkRef) for value in self.dependencies):
            raise TypeError("dependencies must contain WorkRef values")
        object.__setattr__(self, "provider_data", _mapping(self.provider_data))
        if self.provider_version is not None:
            object.__setattr__(
                self,
                "provider_version",
                _text(self.provider_version, "provider_version"),
            )


@dataclass(frozen=True, slots=True)
class ResourcePolicySelection:
    """The local resource identity and guarantee selected for a work item."""

    resource: str
    capability: str
    scope: str
    generic_execution_guarantee: str
    provider_fencing: bool = False

    def __post_init__(self) -> None:
        for field in (
            "resource",
            "capability",
            "scope",
            "generic_execution_guarantee",
        ):
            object.__setattr__(self, field, _text(getattr(self, field), field))


@dataclass(frozen=True, slots=True)
class ProviderReceipt:
    """Durable evidence returned by an authorized provider mutation."""

    source_id: str
    ref: WorkRef | None
    operation: str
    provider_version: str | None
    durable_location: str
    observed_state: Mapping[str, object] | None
    conditional_write: bool
    outcome: ReceiptOutcome = "confirmed"
    fencing_evidence: object | None = None

    def __post_init__(self) -> None:
        object.__setattr__(self, "source_id", _text(self.source_id, "source_id"))
        object.__setattr__(self, "operation", _text(self.operation, "operation"))
        object.__setattr__(
            self, "durable_location", _text(self.durable_location, "durable_location")
        )
        if self.provider_version is not None:
            object.__setattr__(
                self,
                "provider_version",
                _text(self.provider_version, "provider_version"),
            )
        if self.outcome not in ("confirmed", "ambiguous"):
            raise ValueError("outcome must be 'confirmed' or 'ambiguous'")
        object.__setattr__(self, "observed_state", _mapping(self.observed_state))


@dataclass(frozen=True, slots=True)
class CapabilityResult:
    """Explicit supported/unsupported operation result."""

    operation: str
    provider_kind: str
    supported: bool
    reason: str | None = None
    details: Mapping[str, object] | None = None

    def __post_init__(self) -> None:
        object.__setattr__(self, "operation", _text(self.operation, "operation"))
        object.__setattr__(
            self, "provider_kind", _text(self.provider_kind, "provider_kind")
        )
        if self.reason is not None:
            object.__setattr__(self, "reason", _text(self.reason, "reason"))
        object.__setattr__(self, "details", _mapping(self.details))
        if not self.supported and self.reason is None:
            raise ValueError("unsupported capability results require a reason")


@dataclass(frozen=True, slots=True)
class ReviewBoundary:
    """Provider-authorized exact review target."""

    source_id: str
    item_ids: tuple[str, ...]
    group_id: str | None = None

    def __post_init__(self) -> None:
        object.__setattr__(self, "source_id", _text(self.source_id, "source_id"))
        if not isinstance(self.item_ids, tuple):
            object.__setattr__(self, "item_ids", tuple(self.item_ids))
        if not self.item_ids or not all(
            isinstance(item_id, str) and item_id.strip() for item_id in self.item_ids
        ):
            raise ValueError("item_ids must contain at least one non-empty string")
        if self.group_id is not None:
            object.__setattr__(self, "group_id", _text(self.group_id, "group_id"))


type ResolveResult = tuple[Source, ...] | CapabilityResult
type DiscoverResult = tuple[WorkItem, ...] | CapabilityResult
type ReadResult = WorkItem | CapabilityResult
type ResourcePolicyResult = ResourcePolicySelection | CapabilityResult
type MutationResult = ProviderReceipt | CapabilityResult
type ReviewResult = ReviewBoundary | CapabilityResult
