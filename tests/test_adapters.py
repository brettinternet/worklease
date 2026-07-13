from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import cast

from worklease.adapters import key, key_result, load_adapter
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

    def test_markdown_items_share_one_source_claim(self) -> None:
        source = Path(__file__).parents[1] / "README.md"

        first = key("markdown", str(source), "ITEM-1")
        second = key("markdown", str(source), "ITEM-2")

        self.assertEqual(first.resource, second.resource)
        self.assertEqual(first.capability, "source-claim")
        self.assertEqual(first.scope, "source")

    def test_linear_and_unknown_providers_are_deterministic_coordination(self) -> None:
        linear = key("linear", "workspace-uuid/", "issue-uuid")
        repeated = key("linear", "workspace-uuid", "issue-uuid")
        future = key("future-provider", "account-uuid", "work-uuid")

        self.assertEqual(linear.resource, repeated.resource)
        self.assertEqual(linear.capability, "local-coordination")
        self.assertFalse(linear.fenced_mutations)
        self.assertEqual(future.capability, "local-coordination")
        expected_identity = json.dumps(
            {
                "provider": "future-provider",
                "source": "account-uuid",
                "item": "work-uuid",
            },
            sort_keys=True,
            separators=(",", ":"),
        )
        expected_digest = hashlib.sha256(expected_identity.encode()).hexdigest()
        self.assertEqual(
            future.resource, f"coordination:future-provider:{expected_digest}"
        )

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
        for provider in ("github", "backlog-md", "markdown", "linear", "future"):
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
