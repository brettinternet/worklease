"""In-memory source provider used to demonstrate SDK composition."""

from __future__ import annotations

from collections.abc import Mapping, Sequence

from worklease_source_sdk import (
    CONTRACT_VERSION,
    CapabilityResult,
    ProviderReceipt,
    ResourcePolicySelection,
    ReviewBoundary,
    Source,
    SourceProvider,
    WorkItem,
    WorkItemState,
    WorkRef,
)

from .policy import ExamplePolicy


class ExampleProvider(SourceProvider):
    """A complete provider boundary with no scheduler or lease lifecycle."""

    kind = "example-source"
    contract_version = CONTRACT_VERSION

    def __init__(self) -> None:
        self.source = Source(
            id="example-source:demo",
            kind=self.kind,
            locator="demo",
            name="Example source",
        )
        first = WorkRef(self.source.id, "item-1")
        second = WorkRef(self.source.id, "item-2")
        self._items: dict[str, WorkItem] = {
            first.item_id: WorkItem(
                ref=first,
                title="First example item",
                body="A source-qualified prerequisite.",
                dependencies=(),
                state=WorkItemState("open", False, False),
                provider_version="v1",
            ),
            second.item_id: WorkItem(
                ref=second,
                title="Second example item",
                body="A dependent source-qualified item.",
                dependencies=(first,),
                state=WorkItemState("open", False, False),
                provider_version="v1",
            ),
        }
        self._version = "v1"
        self._archived = False

    def resolve(
        self,
        arguments: Sequence[str],
        context: Mapping[str, object],
    ) -> tuple[Source, ...] | CapabilityResult:
        del context
        if not arguments:
            return CapabilityResult(
                "resolve",
                self.kind,
                False,
                "source-required",
            )
        if any(argument != self.source.locator for argument in arguments):
            return CapabilityResult("resolve", self.kind, False, "unknown-source")
        return (self.source,)

    def discover(
        self,
        source: Source,
        selector: str | None = None,
    ) -> tuple[WorkItem, ...] | CapabilityResult:
        if source.id != self.source.id:
            return CapabilityResult("discover", self.kind, False, "unknown-source")
        if selector is None:
            return tuple(self._items.values())
        matches = tuple(
            item
            for item in self._items.values()
            if selector in (item.ref.item_id, item.title, item.body)
        )
        if not matches:
            return CapabilityResult("discover", self.kind, False, "item-not-found")
        dependency_ids = {
            dependency.item_id for item in matches for dependency in item.dependencies
        }
        return tuple(
            item
            for item in self._items.values()
            if item in matches or item.ref.item_id in dependency_ids
        )

    def read_item(self, ref: WorkRef) -> WorkItem | CapabilityResult:
        if ref.source_id != self.source.id:
            return CapabilityResult("read-item", self.kind, False, "unknown-source")
        return self._items.get(
            ref.item_id,
            CapabilityResult("read-item", self.kind, False, "item-not-found"),
        )

    def resource_policy(
        self,
        ref: WorkRef,
        work_key: str,
    ) -> ResourcePolicySelection | CapabilityResult:
        if ref.source_id != self.source.id or ref.item_id not in self._items:
            return CapabilityResult(
                "resource-policy", self.kind, False, "item-not-found"
            )
        del work_key
        key = ExamplePolicy().key(ref.source_id, ref.item_id)
        return ResourcePolicySelection(
            resource=key.resource,
            capability=key.capability,
            scope=key.scope,
            generic_execution_guarantee=key.generic_execution_guarantee,
            provider_fencing=key.provider_fencing,
        )

    def write_state(
        self,
        ref: WorkRef,
        patch: Mapping[str, object],
        authority: object,
        expected_version: str | None = None,
    ) -> ProviderReceipt | CapabilityResult:
        return self._mutate(ref, "write-state", patch, authority, expected_version)

    def record_progress(
        self,
        ref: WorkRef,
        checkpoint: Mapping[str, object],
        authority: object,
        expected_version: str | None = None,
    ) -> ProviderReceipt | CapabilityResult:
        return self._mutate(
            ref,
            "record-progress",
            {"checkpoint": dict(checkpoint)},
            authority,
            expected_version,
        )

    def resolve_review_boundary(
        self,
        source: Source,
        explicit_selector: str | None,
        authority: object,
    ) -> ReviewBoundary | CapabilityResult:
        del authority
        if source.id != self.source.id:
            return CapabilityResult(
                "review-boundary", self.kind, False, "unknown-source"
            )
        selector = explicit_selector or "item-1"
        if selector not in self._items:
            return CapabilityResult(
                "review-boundary", self.kind, False, "item-not-found"
            )
        return ReviewBoundary(source.id, (selector,))

    def archive(
        self,
        target: Source | WorkRef,
        authority: object,
        expected_version: str | None = None,
    ) -> ProviderReceipt | CapabilityResult:
        if authority is None:
            return CapabilityResult("archive", self.kind, False, "authority-required")
        if expected_version is not None and expected_version != self._version:
            return CapabilityResult(
                "archive", self.kind, False, "stale-provider-version"
            )
        if isinstance(target, WorkRef):
            if target.source_id != self.source.id:
                return CapabilityResult("archive", self.kind, False, "unknown-source")
            if target.item_id not in self._items:
                return CapabilityResult("archive", self.kind, False, "item-not-found")
            target_refs = (target,)
        else:
            if target.id != self.source.id:
                return CapabilityResult("archive", self.kind, False, "unknown-source")
            target_refs = tuple(
                WorkRef(self.source.id, item_id) for item_id in self._items
            )
        next_version = f"v{int(self._version.removeprefix('v')) + 1}"
        for ref in target_refs:
            item = self._items[ref.item_id]
            self._items[ref.item_id] = WorkItem(
                ref=item.ref,
                title=item.title,
                body=item.body,
                dependencies=item.dependencies,
                state=WorkItemState("archived", True, False),
                provider_version=next_version,
                provider_data=item.provider_data,
            )
        self._version = next_version
        self._archived = not isinstance(target, WorkRef)
        return ProviderReceipt(
            source_id=self.source.id,
            ref=target if isinstance(target, WorkRef) else None,
            operation="archive",
            provider_version=self._version,
            durable_location=f"example://receipts/{self._version}",
            observed_state={
                "archived": True,
                "itemIds": tuple(ref.item_id for ref in target_refs),
            },
            conditional_write=expected_version is not None,
            fencing_evidence=(
                {"expectedVersion": expected_version}
                if expected_version is not None
                else None
            ),
        )

    def _mutate(
        self,
        ref: WorkRef,
        operation: str,
        patch: Mapping[str, object],
        authority: object,
        expected_version: str | None,
    ) -> ProviderReceipt | CapabilityResult:
        if authority is None:
            return CapabilityResult(operation, self.kind, False, "authority-required")
        item = self._items.get(ref.item_id)
        if item is None or ref.source_id != self.source.id:
            return CapabilityResult(operation, self.kind, False, "item-not-found")
        if expected_version is not None and expected_version != self._version:
            return CapabilityResult(
                operation, self.kind, False, "stale-provider-version"
            )
        status = str(patch.get("status", item.state.status))
        blocker_value = patch.get("blocker", item.state.blocker)
        blocker = blocker_value if isinstance(blocker_value, str) else None
        next_version = f"v{int(self._version.removeprefix('v')) + 1}"
        self._items[ref.item_id] = WorkItem(
            ref=item.ref,
            title=item.title,
            body=item.body,
            dependencies=item.dependencies,
            state=WorkItemState(
                status,
                status in {"done", "archived"},
                bool(patch.get("blocked", item.state.is_blocked)),
                blocker,
            ),
            provider_version=next_version,
            provider_data=item.provider_data,
        )
        self._version = next_version
        return ProviderReceipt(
            source_id=self.source.id,
            ref=ref,
            operation=operation,
            provider_version=self._version,
            durable_location=f"example://receipts/{self._version}",
            observed_state={"status": status, **dict(patch)},
            conditional_write=expected_version is not None,
            fencing_evidence=(
                {"expectedVersion": expected_version}
                if expected_version is not None
                else None
            ),
        )
