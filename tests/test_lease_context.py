from __future__ import annotations

import hashlib
import os
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from worklease.lease_context import (
    context_lease_path,
    prepare_context_lease_path,
    resolve_context_root,
)
from worklease.models import LeaseError
from worklease.sqlite import secure_directory


class LeaseContextTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.home = self.root / "state"

    def git(self, cwd: Path, *arguments: str) -> None:
        environment = {
            key: value
            for key, value in os.environ.items()
            if not key.startswith("GIT_")
        }
        subprocess.run(
            ["git", "-C", str(cwd), *arguments],
            check=True,
            capture_output=True,
            text=True,
            env=environment,
        )

    def initialize_repository(self, path: Path) -> None:
        path.mkdir()
        self.git(path, "init", "-b", "main")
        (path / "tracked").write_text("tracked\n")
        self.git(path, "add", "tracked")
        self.git(
            path,
            "-c",
            "user.name=Test User",
            "-c",
            "user.email=test@example.invalid",
            "commit",
            "-m",
            "initial",
        )

    def assert_unsafe_directory(self, directory: Path) -> None:
        with self.assertRaisesRegex(
            LeaseError, "lease-context-directory-unsafe"
        ) as raised:
            prepare_context_lease_path(self.home, cwd=self.root)
        self.assertEqual(64, raised.exception.code)
        self.assertEqual({}, raised.exception.details)

    def test_non_git_context_uses_resolved_caller_and_read_creates_nothing(
        self,
    ) -> None:
        caller = self.root / "caller"
        caller.mkdir()
        path = context_lease_path(self.home, cwd=caller)
        digest = hashlib.sha256(str(caller.resolve()).encode("utf-8")).hexdigest()

        self.assertEqual(
            self.home.resolve() / "context-leases" / f"{digest}.lease", path
        )
        self.assertEqual(caller.resolve(), resolve_context_root(caller))
        self.assertFalse(self.home.exists())

    def test_non_utf8_context_fails_with_a_stable_error(self) -> None:
        with (
            patch(
                "worklease.lease_context.resolve_context_root",
                return_value=Path("/tmp/non-utf8-\udcff"),
            ),
            self.assertRaisesRegex(
                LeaseError, "lease-context-directory-invalid"
            ) as raised,
        ):
            context_lease_path(self.home)
        self.assertEqual(64, raised.exception.code)

    def test_distinct_non_git_directories_have_distinct_handles(self) -> None:
        first = self.root / "first"
        second = self.root / "second"
        first.mkdir()
        second.mkdir()
        self.assertNotEqual(
            context_lease_path(self.home, cwd=first),
            context_lease_path(self.home, cwd=second),
        )

    @unittest.skipUnless(shutil.which("git"), "git is required")
    def test_git_root_nested_symlink_and_routing_environment_share_context(
        self,
    ) -> None:
        repository = self.root / "repository"
        self.initialize_repository(repository)
        nested = repository / "nested"
        nested.mkdir()
        alias = self.root / "repository-alias"
        alias.symlink_to(repository, target_is_directory=True)

        expected = context_lease_path(self.home, cwd=repository)
        with patch.dict(
            os.environ,
            {"GIT_DIR": str(self.root / "wrong.git"), "GIT_WORK_TREE": str(self.root)},
        ):
            self.assertEqual(expected, context_lease_path(self.home, cwd=nested))
        self.assertEqual(expected, context_lease_path(self.home, cwd=alias))

    @unittest.skipUnless(shutil.which("git"), "git is required")
    def test_linked_worktree_has_a_distinct_context(self) -> None:
        primary = self.root / "primary"
        self.initialize_repository(primary)
        linked = self.root / "linked"
        self.git(primary, "worktree", "add", "-b", "linked", str(linked))

        self.assertNotEqual(
            context_lease_path(self.home, cwd=primary),
            context_lease_path(self.home, cwd=linked),
        )
        self.assertEqual(linked.resolve(), resolve_context_root(linked))

    def test_failed_git_probe_falls_back_to_caller(self) -> None:
        caller = self.root / "caller"
        caller.mkdir()
        with patch("worklease.lease_context._git_output", return_value=None):
            self.assertEqual(caller.resolve(), resolve_context_root(caller))

    def test_prepare_creates_private_directory(self) -> None:
        path = prepare_context_lease_path(self.home, cwd=self.root)
        metadata = path.parent.stat()
        self.assertTrue(stat.S_ISDIR(metadata.st_mode))
        self.assertEqual(0o700, stat.S_IMODE(metadata.st_mode))
        self.assertFalse(path.exists())

    def test_prepare_rejects_symlink_directory(self) -> None:
        destination = self.root / "destination"
        destination.mkdir()
        destination.chmod(0o755)
        self.home.mkdir()
        contextual_directory = self.home / "context-leases"
        contextual_directory.symlink_to(destination, target_is_directory=True)

        self.assert_unsafe_directory(contextual_directory)
        with self.assertRaises(OSError):
            secure_directory(contextual_directory)
        self.assertEqual(0o755, stat.S_IMODE(destination.stat().st_mode))

    def test_prepare_rejects_non_directory(self) -> None:
        self.home.mkdir()
        (self.home / "context-leases").write_text("not a directory\n")
        self.assert_unsafe_directory(self.home / "context-leases")

    def test_prepare_rejects_foreign_owned_directory(self) -> None:
        directory = self.home / "context-leases"
        directory.mkdir(parents=True)
        with patch(
            "worklease.lease_context.os.geteuid",
            return_value=directory.stat().st_uid + 1,
        ):
            self.assert_unsafe_directory(directory)


if __name__ == "__main__":
    unittest.main()
