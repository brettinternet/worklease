from __future__ import annotations

import os
import stat
import tempfile
import unittest
from pathlib import Path

from worklease.credentials import MAX_CREDENTIAL_BYTES, resolve_credential
from worklease.models import LeaseError


class CredentialResolverTests(unittest.TestCase):
    def assert_error(self, expected: str, **sources: object) -> None:
        with self.assertRaises(LeaseError) as context:
            resolve_credential(**sources)
        self.assertEqual(expected, context.exception.reason)
        self.assertEqual({}, context.exception.details)

    def test_direct_credential_is_supported_and_newline_is_removed(self) -> None:
        self.assertEqual("secret", resolve_credential(token="secret\n"))

    def test_sources_are_mutually_exclusive(self) -> None:
        self.assert_error("credential-missing")
        self.assert_error("credential-source-conflict", token="one", token_fd=0)

    def test_direct_credential_rejects_malformed_and_oversized_values(self) -> None:
        self.assert_error("credential-argv-malformed", token="one\ntwo")
        self.assert_error(
            "credential-argv-oversized", token="x" * (MAX_CREDENTIAL_BYTES + 1)
        )

    def test_file_source_requires_owner_only_regular_file(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "token"
            path.write_text("file-secret\n", encoding="utf-8")
            path.chmod(stat.S_IRUSR | stat.S_IWUSR)
            self.assertEqual("file-secret", resolve_credential(token_file=path))

            path.chmod(stat.S_IRUSR | stat.S_IWUSR | stat.S_IRGRP)
            self.assert_error("credential-file-unsafe", token_file=path)

    def test_file_source_rejects_symlinks_and_never_leaks_path(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "target"
            target.write_text("secret", encoding="utf-8")
            link = Path(directory) / "link"
            link.symlink_to(target)
            self.assert_error("credential-file-unreadable", token_file=link)
            with self.assertRaises(LeaseError) as context:
                resolve_credential(token_file=link)
            self.assertNotIn(str(link), str(context.exception))

    def test_fd_source_duplicates_descriptor_and_supports_stdin(self) -> None:
        read_fd, write_fd = os.pipe()
        try:
            os.write(write_fd, b"fd-secret\n")
        finally:
            os.close(write_fd)
        try:
            self.assertEqual("fd-secret", resolve_credential(token_fd=read_fd))
        finally:
            os.close(read_fd)

    def test_fd_source_rejects_bad_descriptors_and_malformed_values(self) -> None:
        self.assert_error("credential-fd-malformed", token_fd=-1)
        read_fd, write_fd = os.pipe()
        try:
            os.write(write_fd, b"one\ntwo")
        finally:
            os.close(write_fd)
        try:
            self.assert_error("credential-fd-malformed", token_fd=read_fd)
        finally:
            os.close(read_fd)
