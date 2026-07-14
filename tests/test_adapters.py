from __future__ import annotations

import hashlib
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import ExitStack
from pathlib import Path
from typing import cast
from unittest.mock import patch

from worklease.adapters import (
    ProviderAdapter,
    ResourcePolicyDescriptor,
    ResourcePolicyRegistration,
    available_policy_names,
    key,
    key_result,
    load_adapter,
    run_policy_conformance,
)
from worklease.adapters import registry as policy_registry
from worklease.adapters.markdown import MarkdownAdapter
from worklease.models import AcquireRequest, LeaseError, MutationRequest
from worklease.store import LeaseStore


class AdapterKeyTests(unittest.TestCase):
    def test_importing_facade_does_not_eagerly_load_provider_modules(self) -> None:
        code = (
            "import sys; import worklease.adapters; "
            "assert not any(name.startswith('worklease.adapters.') and "
            "name.rsplit('.', 1)[-1] in {'github', 'backlog_md', 'markdown', 'linear'} "
            "for name in sys.modules)"
        )
        result = subprocess.run(
            [sys.executable, "-c", code], capture_output=True, text=True, check=False
        )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_github_key_matches_reference_policy(self) -> None:
        result = key("github", "GitHub.com/Example/Repo.git", "115")

        self.assertEqual(result.resource, "github:github.com/example/repo#115")
        self.assertEqual(result.capability, "item-claim")
        self.assertEqual(result.scope, "item")
        self.assertTrue(result.fenced_mutations)
        self.assertFalse(result.provider_fencing)
        self.assertEqual(result.generic_execution_guarantee, "local-coordination")
        self.assertEqual(
            load_adapter("github").generic_execution_guarantee,
            "local-coordination",
        )

        downgraded = key(
            "github", "github.com/example/repo", "115", coordination_only=True
        )
        self.assertEqual(downgraded.resource, result.resource)
        self.assertEqual(downgraded.capability, "local-coordination")
        self.assertFalse(downgraded.fenced_mutations)

    def test_backlog_and_markdown_use_repository_local_identity(self) -> None:
        source = Path(__file__).parents[1] / "docs" / "backlog"
        backlog = key("backlog-md", str(source), "TASK-1")
        markdown = key("markdown", str(source / "config.yml"), "ITEM-1")

        self.assertTrue(backlog.resource.startswith("backlog-md:"))
        self.assertTrue(backlog.resource.endswith(":docs/backlog:TASK-1"))
        self.assertEqual(backlog.capability, "item-claim")
        self.assertTrue(markdown.resource.startswith("markdown:"))
        self.assertTrue(
            markdown.resource.endswith(":docs/backlog/config.yml:__source__")
        )
        self.assertEqual(markdown.capability, "source-claim")
        self.assertEqual(markdown.scope, "source")

    def test_missing_git_uses_path_fallback(self) -> None:
        with patch(
            "worklease.adapters.protocol.subprocess.run",
            side_effect=FileNotFoundError,
        ):
            result = key("backlog-md", "/tmp/worklease-project", "TASK-1")
        self.assertTrue(result.resource.startswith("backlog-md:"))

    def test_nested_source_keys_match_across_linked_worktrees(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "repo"
            git_environment = {
                name: value
                for name, value in os.environ.items()
                if not name.startswith("GIT_")
            }
            source = root / "docs" / "backlog"
            source.mkdir(parents=True)
            (source / "source.md").write_text("# source\n")
            subprocess.run(
                ["git", "-C", str(root), "init", "--quiet"],
                check=True,
                env=git_environment,
            )
            subprocess.run(
                ["git", "-C", str(root), "add", "."],
                check=True,
                env=git_environment,
            )
            subprocess.run(
                [
                    "git",
                    "-C",
                    str(root),
                    "-c",
                    "user.name=worklease-test",
                    "-c",
                    "user.email=worklease-test@example.invalid",
                    "commit",
                    "--quiet",
                    "-m",
                    "initial",
                ],
                check=True,
                env=git_environment,
            )
            linked = Path(directory) / "linked"
            subprocess.run(
                [
                    "git",
                    "-C",
                    str(root),
                    "worktree",
                    "add",
                    "--quiet",
                    "--detach",
                    str(linked),
                    "HEAD",
                ],
                check=True,
                env=git_environment,
            )
            try:
                main_backlog = key(
                    "backlog-md", str(root / "docs" / "backlog"), "TASK-1"
                )
                linked_backlog = key(
                    "backlog-md", str(linked / "docs" / "backlog"), "TASK-1"
                )
                main_markdown = key(
                    "markdown",
                    str(root / "docs" / "backlog" / "source.md"),
                    "ITEM-1",
                )
                linked_markdown = key(
                    "markdown",
                    str(linked / "docs" / "backlog" / "source.md"),
                    "ITEM-1",
                )
            finally:
                subprocess.run(
                    [
                        "git",
                        "-C",
                        str(root),
                        "worktree",
                        "remove",
                        "--force",
                        str(linked),
                    ],
                    check=True,
                    env=git_environment,
                )

            self.assertEqual(main_backlog.resource, linked_backlog.resource)
            self.assertEqual(main_markdown.resource, linked_markdown.resource)

    def test_generic_policy_is_explicit_and_unknown_names_fail(self) -> None:
        generic = key("generic", "account-uuid", "work-uuid")
        self.assertEqual(generic.capability, "local-coordination")
        self.assertFalse(generic.fenced_mutations)
        with self.assertRaises(LeaseError) as raised:
            key("future-provider", "account-uuid", "work-uuid")
        self.assertEqual(raised.exception.reason, "resource-policy-not-found")
        self.assertEqual(raised.exception.details["schemaVersion"], 1)
        self.assertIn("generic", raised.exception.details["available"])

    def test_policy_descriptors_are_lazy_and_versioned(self) -> None:
        self.assertEqual(
            set(available_policy_names()),
            {"backlog-md", "generic", "github", "linear", "markdown"},
        )
        descriptor = policy_registry.load_policy("generic").descriptor
        self.assertEqual(descriptor.contract_version, 1)
        self.assertEqual(
            descriptor.to_dict()["genericExecutionGuarantee"], "local-coordination"
        )

        report = run_policy_conformance(
            "generic",
            source="account",
            items=("item-a", "item-b"),
        )
        self.assertTrue(report.passed, report.failures)
        self.assertEqual(
            {
                "contract-version",
                "key-policy-version",
                "collision-avoidance",
                "identity-stability",
                "process-stability",
                "worktree-stability",
            },
            set(report.checks),
        )

    def test_source_scoped_policy_conformance_preserves_source_identity(self) -> None:
        report = run_policy_conformance(
            "markdown",
            source="/tmp/worklease-source.md",
            items=("item-a", "item-b"),
        )
        self.assertTrue(report.passed, report.failures)
        self.assertIn("scope-semantics", report.checks)

    def test_external_policy_registration_and_failures_are_deterministic(self) -> None:
        descriptor = ResourcePolicyDescriptor(
            name="external",
            origin="fixture",
            origin_version="1.2",
            capability="item-claim",
        )
        registration = ResourcePolicyRegistration(
            descriptor, lambda provider: load_adapter("github")
        )

        class EntryPoint:
            def __init__(self, loaded: object, *, name: str = "external") -> None:
                self.name = name
                self.value = "fixture:policy"
                self._loaded = loaded

            def load(self) -> object:
                if isinstance(self._loaded, BaseException):
                    raise self._loaded
                return self._loaded

        class EntryPoints:
            def __init__(self, values: list[EntryPoint]) -> None:
                self.values = values

            def select(self, *, group: str) -> list[EntryPoint]:
                self.assert_group = group
                return self.values

        with patch.object(
            policy_registry.metadata,
            "entry_points",
            return_value=EntryPoints([EntryPoint(registration)]),
        ):
            self.assertEqual(load_adapter("external").provider, "github")

        with (
            patch.object(
                policy_registry.metadata,
                "entry_points",
                return_value=EntryPoints(
                    [EntryPoint(registration), EntryPoint(registration)]
                ),
            ),
            self.assertRaisesRegex(LeaseError, "resource-policy-duplicate"),
        ):
            load_adapter("external")
        with (
            patch.object(
                policy_registry.metadata,
                "entry_points",
                return_value=EntryPoints([EntryPoint(registration, name="github")]),
            ),
            self.assertRaisesRegex(LeaseError, "resource-policy-duplicate"),
        ):
            load_adapter("github")

        incompatible = ResourcePolicyRegistration(
            ResourcePolicyDescriptor(
                name="external",
                origin="fixture",
                origin_version="1.2",
                contract_version=2,
            ),
            registration.factory,
        )
        with (
            patch.object(
                policy_registry.metadata,
                "entry_points",
                return_value=EntryPoints([EntryPoint(incompatible)]),
            ),
            self.assertRaisesRegex(LeaseError, "resource-policy-contract-version"),
        ):
            load_adapter("external")

        def failing_factory(provider: str) -> ProviderAdapter:
            del provider
            raise LeaseError("plugin-specific", code=64)

        failing = ResourcePolicyRegistration(descriptor, failing_factory)
        with ExitStack() as stack:
            stack.enter_context(
                patch.object(
                    policy_registry.metadata,
                    "entry_points",
                    return_value=EntryPoints([EntryPoint(failing)]),
                )
            )
            raised = stack.enter_context(
                self.assertRaisesRegex(LeaseError, "resource-policy-factory-failed")
            )
            load_adapter("external")
        self.assertEqual(raised.exception.details["schemaVersion"], 1)

        with (
            patch.object(
                policy_registry.metadata,
                "entry_points",
                return_value=EntryPoints([EntryPoint(RuntimeError("boom"))]),
            ),
            self.assertRaisesRegex(LeaseError, "resource-policy-load-failed"),
        ):
            load_adapter("external")

        with (
            patch.object(
                policy_registry.metadata,
                "entry_points",
                return_value=EntryPoints([EntryPoint(object())]),
            ),
            self.assertRaisesRegex(LeaseError, "resource-policy-invalid-descriptor"),
        ):
            load_adapter("external")

    def test_key_result_has_reference_compatible_fields(self) -> None:
        result = key_result("linear", "workspace", "issue")

        self.assertEqual(
            set(result),
            {
                "ok",
                "operation",
                "provider",
                "capability",
                "scope",
                "fencedMutations",
                "providerFencing",
                "genericExecutionGuarantee",
                "resource",
            },
        )
        self.assertTrue(result["ok"])
        self.assertEqual(result["operation"], "key")

    def test_provider_modules_are_loaded_only_when_requested(self) -> None:
        adapter = load_adapter("github")
        self.assertEqual(adapter.provider, "github")
        self.assertNotIn("gh", sys.modules)

    def test_bundled_adapters_reject_provider_fencing(self) -> None:
        for provider in ("github", "backlog-md", "markdown", "linear", "generic"):
            with self.subTest(provider=provider):
                adapter = load_adapter(provider)
                with self.assertRaises(LeaseError) as raised:
                    adapter.require_provider_fence(lambda: True)
                self.assertEqual(
                    raised.exception.reason, "unsupported-provider-fencing"
                )


class MarkdownReplacementTests(unittest.TestCase):
    def test_markdown_adapter_delegates_expected_hash_replacement(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source.md"
            candidate = root / "candidate.md"
            source.write_text("old\n")
            candidate.write_text("new\n")
            store = LeaseStore(root / "state")
            adapter = cast(MarkdownAdapter, load_adapter("markdown"))
            resource = adapter.key(str(source), "ITEM-1").resource
            acquired = store.acquire(
                AcquireRequest(
                    resource=resource,
                    claim_id="claim-markdown",
                    agent_id="agent",
                    session_id="session",
                    owner_id="owner",
                    work_key="replace-file:ITEM-1",
                )
            )
            claim = acquired["claim"]
            request = MutationRequest(
                resource=resource,
                claim_id=str(claim["claimId"]),
                token=str(claim["token"]),
                revision=int(claim["revision"]),
                operation_id="replace-1",
            )

            receipt = adapter.replace_file(
                store,
                request,
                source,
                hashlib.sha256(b"old\n").hexdigest(),
                candidate,
            )

            self.assertTrue(receipt["ok"])
            self.assertEqual(source.read_text(), "new\n")
            self.assertEqual(receipt["operation"], "replace-file")
            retry = adapter.replace_file(
                store,
                request,
                source,
                hashlib.sha256(b"old\n").hexdigest(),
                candidate,
            )
            self.assertTrue(retry["idempotent"])

    def test_markdown_coordination_only_claim_cannot_replace(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source.md"
            candidate = root / "candidate.md"
            source.write_text("old\n")
            candidate.write_text("new\n")
            store = LeaseStore(root / "state")
            adapter = cast(MarkdownAdapter, load_adapter("markdown"))
            resource = adapter.key(
                str(source), "ITEM-1", coordination_only=True
            ).resource
            acquired = store.acquire(
                AcquireRequest(
                    resource=resource,
                    claim_id="claim-coordination",
                    agent_id="agent",
                    session_id="session",
                    owner_id="owner",
                    work_key="replace-file:ITEM-1",
                    coordination_only=True,
                )
            )
            claim = acquired["claim"]
            request = MutationRequest(
                resource=resource,
                claim_id=str(claim["claimId"]),
                token=str(claim["token"]),
                revision=int(claim["revision"]),
                operation_id="replace-coordination",
            )

            with self.assertRaises(LeaseError) as raised:
                adapter.replace_file(
                    store,
                    request,
                    source,
                    hashlib.sha256(b"old\n").hexdigest(),
                    candidate,
                )

            self.assertEqual(
                raised.exception.reason, "unsupported-coordination-replace-file"
            )
            self.assertEqual(source.read_text(), "old\n")


if __name__ == "__main__":
    unittest.main()
