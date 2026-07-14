from __future__ import annotations

import unittest
from collections.abc import Mapping

from worklease_source_sdk import (
    CONTRACT_VERSION,
    CapabilityResult,
    ProviderConformanceCase,
    ProviderReceipt,
    ResourcePolicySelection,
    ReviewBoundary,
    Source,
    WorkItem,
    WorkItemState,
    WorkRef,
    run_provider_conformance,
)


class ExampleProvider:
    kind = "example"
    contract_version = CONTRACT_VERSION

    def __init__(
        self, source: Source, items: tuple[WorkItem, ...], *, ambiguous: bool = False
    ):
        self.source = source
        self.items = items
        self.ambiguous = ambiguous
        self.version = "v1"

    def resolve(self, arguments: tuple[str, ...], context: Mapping[str, object]):
        del arguments, context
        return (self.source,)

    def discover(self, source: Source, selector: str | None = None):
        del selector
        return self.items if source.id == self.source.id else ()

    def read_item(self, ref: WorkRef):
        for item in self.items:
            if item.ref == ref:
                return item
        return CapabilityResult("read-item", self.kind, False, "not-found")

    def resource_policy(self, ref: WorkRef, work_key: str):
        del work_key
        return ResourcePolicySelection(
            resource=f"{self.kind}:{ref.source_id}:{ref.item_id}",
            capability="local-host",
            scope="item",
            generic_execution_guarantee="local-coordination",
        )

    def write_state(
        self,
        ref: WorkRef,
        patch: Mapping[str, object],
        authority: object,
        expected_version: str | None = None,
    ):
        del authority
        if expected_version != self.version:
            return CapabilityResult(
                "write-state", self.kind, False, "stale-provider-version"
            )
        next_version = f"v{int(self.version.removeprefix('v')) + 1}"
        self.version = next_version
        return ProviderReceipt(
            source_id=ref.source_id,
            ref=ref,
            operation="write-state",
            provider_version=next_version,
            durable_location="example://receipts/write-state/1",
            observed_state=dict(patch),
            conditional_write=True,
            outcome="ambiguous" if self.ambiguous else "confirmed",
            fencing_evidence="example-cas",
        )

    def record_progress(
        self,
        ref: WorkRef,
        checkpoint: Mapping[str, object],
        authority: object,
        expected_version: str | None = None,
    ):
        del authority
        if expected_version != self.version:
            return CapabilityResult(
                "record-progress", self.kind, False, "stale-provider-version"
            )
        next_version = f"v{int(self.version.removeprefix('v')) + 1}"
        self.version = next_version
        return ProviderReceipt(
            source_id=ref.source_id,
            ref=ref,
            operation="record-progress",
            provider_version=next_version,
            durable_location="example://receipts/progress/1",
            observed_state=dict(checkpoint),
            conditional_write=True,
            fencing_evidence="example-cas",
        )

    def resolve_review_boundary(
        self,
        source: Source,
        explicit_selector: str | None,
        authority: object,
    ):
        del explicit_selector, authority
        return ReviewBoundary(source.id, (self.items[-1].ref.item_id,))

    def archive(
        self,
        target: Source | WorkRef,
        authority: object,
        expected_version: str | None = None,
    ):
        del authority
        if expected_version != self.version:
            return CapabilityResult(
                "archive", self.kind, False, "stale-provider-version"
            )
        next_version = f"v{int(self.version.removeprefix('v')) + 1}"
        self.version = next_version
        ref = target if isinstance(target, WorkRef) else None
        return ProviderReceipt(
            source_id=self.source.id,
            ref=ref,
            operation="archive",
            provider_version=next_version,
            durable_location="example://receipts/archive/1",
            observed_state={"archived": True},
            conditional_write=True,
            fencing_evidence="example-cas",
        )


class ReadOnlyProvider(ExampleProvider):
    def resource_policy(self, ref, work_key):
        del ref, work_key
        return CapabilityResult("resource-policy", self.kind, False, "read-only")

    def write_state(self, ref, patch, authority, expected_version=None):
        del ref, patch, authority, expected_version
        return CapabilityResult("write-state", self.kind, False, "read-only")

    def record_progress(self, ref, checkpoint, authority, expected_version=None):
        del ref, checkpoint, authority, expected_version
        return CapabilityResult("record-progress", self.kind, False, "read-only")

    def resolve_review_boundary(self, source, explicit_selector, authority):
        del source, explicit_selector, authority
        return CapabilityResult("review-boundary", self.kind, False, "unsupported")

    def archive(self, target, authority, expected_version=None):
        del target, authority, expected_version
        return CapabilityResult("archive", self.kind, False, "read-only")


class LeakingProvider(ExampleProvider):
    def write_state(self, ref, patch, authority, expected_version=None):
        del authority, expected_version
        return ProviderReceipt(
            source_id=ref.source_id,
            ref=ref,
            operation="write-state",
            provider_version="v2",
            durable_location="example://receipts/write-state/secret-token",
            observed_state=dict(patch),
            conditional_write=False,
        )


class UntruthfulProvider(LeakingProvider):
    def resource_policy(self, ref, work_key):
        policy = super().resource_policy(ref, work_key)
        return ResourcePolicySelection(
            resource=policy.resource,
            capability=policy.capability,
            scope=policy.scope,
            generic_execution_guarantee=policy.generic_execution_guarantee,
            provider_fencing=True,
        )


class EmptyArchiveProvider(ExampleProvider):
    def archive(self, target, authority, expected_version=None):
        del authority, expected_version
        ref = target if isinstance(target, WorkRef) else None
        return ProviderReceipt(
            source_id=self.source.id,
            ref=ref,
            operation="archive",
            provider_version="v2",
            durable_location="example://receipts/archive/1",
            observed_state={},
            conditional_write=False,
        )


class ProviderConformanceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.source = Source("source-1", "example", "example://source", "Example")
        root = WorkItem(
            WorkRef(self.source.id, "root"),
            "Root",
            "root body",
            (),
            WorkItemState("done", True, False),
            provider_version="v1",
        )
        self.item = WorkItem(
            WorkRef(self.source.id, "child"),
            "Child",
            "child body",
            (root.ref,),
            WorkItemState("ready", False, False),
            provider_version="v1",
        )
        self.items = (root, self.item)

    def case(self, **kwargs: object) -> ProviderConformanceCase:
        values: dict[str, object] = {
            "source": self.source,
            "item": self.item,
            "discovered": self.items,
            "secrets": ("secret-token",),
        }
        values.update(kwargs)
        return ProviderConformanceCase(**values)

    def test_good_provider_covers_boundary_and_stale_rejection(self) -> None:
        report = run_provider_conformance(
            ExampleProvider(self.source, self.items), self.case()
        )

        self.assertTrue(report.passed, report.failures)
        self.assertIn("source-qualification", report.checks)
        self.assertIn("dependency-closure", report.checks)
        self.assertIn("stale-version-rejection", report.checks)
        self.assertIn("write-receipt", report.checks)
        self.assertIn("progress-receipt", report.checks)
        self.assertIn("archive-receipt", report.checks)

    def test_unsupported_operations_are_explicit(self) -> None:
        report = run_provider_conformance(
            ReadOnlyProvider(self.source, self.items),
            self.case(
                unsupported_operations=frozenset(
                    {
                        "write-state",
                        "record-progress",
                        "resource-policy",
                        "review-boundary",
                        "archive",
                    }
                )
            ),
        )

        self.assertTrue(report.passed, report.failures)
        self.assertEqual(
            {
                "unsupported-capability:write-state",
                "unsupported-capability:record-progress",
                "unsupported-capability:resource-policy",
                "unsupported-capability:review-boundary",
                "unsupported-capability:archive",
            },
            {
                check
                for check in report.checks
                if check.startswith("unsupported-capability:")
            },
        )

    def test_ambiguous_receipt_is_preserved(self) -> None:
        report = run_provider_conformance(
            ExampleProvider(self.source, self.items, ambiguous=True),
            self.case(ambiguous_operations=frozenset({"write-state"})),
        )

        self.assertTrue(report.passed, report.failures)
        self.assertIn("ambiguous-outcome", report.checks)

    def test_receipt_secret_is_rejected(self) -> None:
        report = run_provider_conformance(
            LeakingProvider(self.source, self.items), self.case()
        )

        self.assertFalse(report.passed)
        self.assertIn("token-redaction", report.failures)

    def test_archive_receipt_must_prove_state(self) -> None:
        report = run_provider_conformance(
            EmptyArchiveProvider(self.source, self.items), self.case()
        )

        self.assertFalse(report.passed)
        self.assertIn("archive-receipt-state", report.failures)

    def test_provider_fencing_declaration_requires_evidence(self) -> None:
        report = run_provider_conformance(
            UntruthfulProvider(self.source, self.items), self.case()
        )

        self.assertFalse(report.passed)
        self.assertIn("truthful-fencing-declaration", report.failures)

    def test_missing_dependency_fails_closure(self) -> None:
        report = run_provider_conformance(
            ExampleProvider(self.source, (self.item,)),
            self.case(discovered=(self.item,)),
        )

        self.assertFalse(report.passed)
        self.assertIn("dependency-closure", report.failures)

    def test_discovery_must_cover_expected_collection(self) -> None:
        independent = WorkItem(
            WorkRef(self.source.id, "independent"),
            "Independent",
            "independent body",
            (),
            WorkItemState("ready", False, False),
            provider_version="v1",
        )
        report = run_provider_conformance(
            ExampleProvider(self.source, self.items),
            self.case(discovered=(*self.items, independent)),
        )

        self.assertFalse(report.passed)
        self.assertIn("complete-discovery", report.failures)


if __name__ == "__main__":
    unittest.main()
