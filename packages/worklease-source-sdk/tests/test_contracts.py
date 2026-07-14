from __future__ import annotations

import unittest

from worklease_source_sdk import (
    CONTRACT_VERSION,
    CapabilityResult,
    ProviderReceipt,
    Source,
    SourceProvider,
    WorkItem,
    WorkItemState,
    WorkRef,
)


class FakeProvider:
    kind = "example"
    contract_version = CONTRACT_VERSION

    def resolve(self, arguments, context):
        del arguments, context
        return ()

    def discover(self, source, selector=None):
        del source, selector
        return ()

    def read_item(self, ref):
        del ref
        return CapabilityResult("read-item", self.kind, False, "not-found")

    def resource_policy(self, ref, work_key):
        del ref, work_key
        return CapabilityResult("resource-policy", self.kind, False, "unsupported")

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


class ContractSmokeTests(unittest.TestCase):
    def test_versioned_immutable_models_preserve_qualified_identity(self) -> None:
        source = Source("source-1", "example", "collection", "Example")
        ref = WorkRef(source.id, "item-1")
        item = WorkItem(
            ref,
            "Item",
            "body",
            (),
            WorkItemState("open", False, False),
            provider_version="v7",
        )

        self.assertEqual(CONTRACT_VERSION, 1)
        self.assertEqual(item.ref, ref)
        self.assertEqual(item.provider_version, "v7")
        with self.assertRaises(AttributeError):
            source.id = "other"  # type: ignore[misc]

    def test_receipt_records_ambiguous_provider_outcome(self) -> None:
        receipt = ProviderReceipt(
            source_id="source-1",
            ref=None,
            operation="write-state",
            provider_version="v8",
            durable_location="example://receipt/8",
            observed_state={"status": "open"},
            conditional_write=False,
            outcome="ambiguous",
        )

        self.assertEqual(receipt.outcome, "ambiguous")
        with self.assertRaises(ValueError):
            ProviderReceipt(
                "source-1",
                None,
                "write-state",
                "v8",
                "example://receipt/8",
                {},
                False,
                outcome="unknown",  # type: ignore[arg-type]
            )

    def test_unsupported_capability_is_explicit(self) -> None:
        result = CapabilityResult(
            operation="archive",
            provider_kind="example",
            supported=False,
            reason="provider-read-only",
        )
        self.assertFalse(result.supported)
        self.assertEqual(result.reason, "provider-read-only")

    def test_protocol_runtime_conformance_is_structural(self) -> None:
        self.assertIsInstance(FakeProvider(), SourceProvider)


if __name__ == "__main__":
    unittest.main()
