from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import tomllib
import unittest
import zipfile
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = ROOT / "tests" / "fixtures" / "resource-policy-plugin"


class ExternalPolicyPackagingTests(unittest.TestCase):
    def run_policy_probe(
        self, install: Path, *, source: Path | None = None
    ) -> dict[str, Any]:
        environment = os.environ.copy()
        paths = [str(install), str(ROOT / "src")]
        if source is not None:
            paths.insert(0, str(source))
        environment["PYTHONPATH"] = os.pathsep.join(paths)
        probe = (
            "import json; "
            "from worklease.adapters import describe_policy, key; "
            "descriptor = describe_policy('fixture').to_dict(); "
            "resource = key('fixture', 'account', 'item').to_dict(); "
            "print(json.dumps({'descriptor': descriptor, 'resource': resource}))"
        )
        result = subprocess.run(
            [sys.executable, "-c", probe],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
        )
        self.assertEqual(0, result.returncode, result.stderr)
        return json.loads(result.stdout)

    def test_fixture_declares_versioned_entry_point(self) -> None:
        project = tomllib.loads((FIXTURE / "pyproject.toml").read_text())

        self.assertEqual(
            {"fixture": "fixture_policy:registration"},
            project["project"]["entry-points"]["worklease.resource_policies"],
        )
        self.assertEqual("1.2.3", project["project"]["version"])

    def test_wheel_install_discovers_external_policy(self) -> None:
        self.assertIsNotNone(shutil.which("uv"))
        with tempfile.TemporaryDirectory() as directory:
            temporary = Path(directory)
            output = temporary / "dist"
            output.mkdir()
            result = subprocess.run(
                ["uv", "build", "--wheel", "--out-dir", str(output)],
                cwd=FIXTURE,
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(0, result.returncode, result.stderr)
            wheels = tuple(output.glob("*.whl"))
            self.assertEqual(1, len(wheels))
            install = temporary / "wheel-install"
            install.mkdir()
            with zipfile.ZipFile(wheels[0]) as archive:
                archive.extractall(install)
            payload = self.run_policy_probe(install)

        self.assertEqual("worklease-policy-fixture", payload["descriptor"]["origin"])
        self.assertEqual("fixture:account#item", payload["resource"]["resource"])

    def test_editable_install_discovers_external_policy(self) -> None:
        self.assertIsNotNone(shutil.which("uv"))
        with tempfile.TemporaryDirectory() as directory:
            temporary = Path(directory)
            install = temporary / "editable-install"
            result = subprocess.run(
                [
                    "uv",
                    "pip",
                    "install",
                    "--target",
                    str(install),
                    "--no-deps",
                    "-e",
                    str(FIXTURE),
                ],
                cwd=ROOT,
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(0, result.returncode, result.stderr)
            payload = self.run_policy_probe(install, source=FIXTURE)

        self.assertEqual("1.2.3", payload["descriptor"]["originVersion"])
        self.assertEqual("item-claim", payload["resource"]["capability"])


if __name__ == "__main__":
    unittest.main()
