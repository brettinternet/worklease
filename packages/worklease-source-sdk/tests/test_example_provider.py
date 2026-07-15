from __future__ import annotations

import subprocess
import sys
import tomllib
import unittest
from pathlib import Path

from source_provider_example import ExampleProvider, registration
from worklease_source_sdk import (
    CapabilityResult,
    ProviderReceipt,
    ResourcePolicySelection,
    WorkItem,
    WorkRef,
)

EXAMPLE_ROOT = Path(__file__).parents[1] / "examples" / "source-provider-plugin"


class ExampleProviderTests(unittest.TestCase):
    def test_package_declares_sdk_and_policy_entry_point(self) -> None:
        with (EXAMPLE_ROOT / "pyproject.toml").open("rb") as stream:
            project = tomllib.load(stream)["project"]
        self.assertIn("worklease-source-sdk>=0.1,<1", project["dependencies"])
        self.assertEqual(
            "source_provider_example.policy:registration",
            project["entry-points"]["worklease.resource_policies"]["example-source"],
        )

    def test_provider_returns_source_qualified_dependency_closure(self) -> None:
        provider = ExampleProvider()
        resolved = provider.resolve(("demo",), {})
        self.assertEqual((provider.source,), resolved)
        discovered = provider.discover(provider.source, "item-2")
        assert isinstance(discovered, tuple)
        self.assertEqual(
            ("item-1", "item-2"), tuple(item.ref.item_id for item in discovered)
        )
        self.assertEqual("item-1", discovered[1].dependencies[0].item_id)

    def test_policy_composes_public_task10_surface_without_provider_fencing(
        self,
    ) -> None:
        provider = ExampleProvider()
        ref = WorkRef(provider.source.id, "item-1")
        policy = provider.resource_policy(ref, "implement:item-1")
        self.assertIsInstance(policy, ResourcePolicySelection)
        assert isinstance(policy, ResourcePolicySelection)
        self.assertEqual("example-source:demo#item-1", policy.resource)
        self.assertEqual(
            policy.resource,
            registration.factory("example-source").key("demo", "item-1").resource,
        )
        self.assertFalse(policy.provider_fencing)
        self.assertEqual(1, registration.descriptor.contract_version)
        self.assertEqual("item-claim", registration.descriptor.capability)

    def test_authoritative_version_and_receipt_behavior(self) -> None:
        provider = ExampleProvider()
        ref = WorkRef(provider.source.id, "item-1")
        stale = provider.write_state(ref, {"status": "done"}, object(), "v0")
        self.assertIsInstance(stale, CapabilityResult)
        assert isinstance(stale, CapabilityResult)
        self.assertEqual("stale-provider-version", stale.reason)
        receipt = provider.write_state(ref, {"status": "done"}, object(), "v1")
        self.assertIsInstance(receipt, ProviderReceipt)
        assert isinstance(receipt, ProviderReceipt)
        self.assertEqual("v2", receipt.provider_version)
        self.assertTrue(receipt.conditional_write)
        self.assertEqual("example://receipts/v2", receipt.durable_location)
        updated = provider.read_item(ref)
        self.assertIsInstance(updated, WorkItem)
        assert isinstance(updated, WorkItem)
        self.assertEqual("v2", updated.provider_version)

    def test_archive_requires_authority_and_persists_item_state(self) -> None:
        provider = ExampleProvider()
        ref = WorkRef(provider.source.id, "item-1")
        denied = provider.archive(ref, None, "v1")
        self.assertIsInstance(denied, CapabilityResult)
        assert isinstance(denied, CapabilityResult)
        self.assertEqual("authority-required", denied.reason)
        receipt = provider.archive(ref, object(), "v1")
        self.assertIsInstance(receipt, ProviderReceipt)
        archived = provider.read_item(ref)
        self.assertIsInstance(archived, WorkItem)
        assert isinstance(archived, WorkItem)
        self.assertTrue(archived.state.is_terminal)
        self.assertEqual("archived", archived.state.status)
        self.assertEqual("v2", archived.provider_version)

    def test_sdk_import_does_not_import_lease_core(self) -> None:
        source_root = Path(__file__).parents[1] / "src"
        result = subprocess.run(
            [
                sys.executable,
                "-c",
                "import sys; import worklease_source_sdk; assert 'worklease' not in sys.modules",
            ],
            env={"PATH": str(source_root), "PYTHONPATH": str(source_root)},
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(0, result.returncode, result.stderr)


if __name__ == "__main__":
    unittest.main()
