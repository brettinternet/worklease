from __future__ import annotations

import json
import subprocess
import sys
import unittest
from importlib.metadata import version

from worklease import __version__


class PackageSmokeTests(unittest.TestCase):
    def run_cli(self, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, "-m", "worklease.cli", *args],
            check=False,
            capture_output=True,
            text=True,
        )

    def test_package_metadata_is_the_cli_version(self) -> None:
        self.assertEqual(__version__, version("worklease"))

    def test_version_defaults_to_schema_versioned_json(self) -> None:
        result = self.run_cli("--version")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")
        self.assertEqual(
            json.loads(result.stdout),
            {
                "schemaVersion": 1,
                "operation": "version",
                "ok": True,
                "version": __version__,
            },
        )

    def test_text_version_is_bare(self) -> None:
        result = self.run_cli("--format", "text", "--version")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, f"{__version__}\n")
        self.assertEqual(result.stderr, "")


if __name__ == "__main__":
    unittest.main()
