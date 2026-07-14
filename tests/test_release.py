from __future__ import annotations

import hashlib
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from scripts.release_artifacts import validate_checksums, write_checksums
from scripts.release_installer import (
    ReleaseError,
    native_asset_name,
    parse_checksums,
    select_asset,
    verify_checksum,
)

ROOT = Path(__file__).resolve().parents[1]
VERSION = "v0.1.0"
EXPECTED_VERSION = "0.1.0"


class ReleaseValidationTests(unittest.TestCase):
    def run_installer(
        self, release: Path, install_directory: Path
    ) -> subprocess.CompletedProcess[str]:
        environment = os.environ.copy()
        environment.update(
            {
                "WORKLEASE_REPOSITORY": "example/worklease",
                "WORKLEASE_RELEASE_BASE_URL": release.as_uri() + "/",
                "WORKLEASE_PLATFORM_SYSTEM": "Linux",
                "WORKLEASE_PLATFORM_MACHINE": "x86_64",
                "WORKLEASE_INSTALL_DIR": str(install_directory),
            }
        )
        return subprocess.run(
            [str(ROOT / "scripts/install-release.sh"), f"VERSION={VERSION}"],
            cwd=ROOT,
            env=environment,
            check=False,
            capture_output=True,
            text=True,
        )

    def test_native_selection_is_exact_and_wheel_fallback_is_explicit(self) -> None:
        native = native_asset_name(VERSION, system="Linux", machine="x86_64")
        wheel = "worklease-0.1.0-py3-none-any.whl"
        selected = select_asset(
            VERSION,
            {native: "a" * 64, wheel: "b" * 64},
            system="Linux",
            machine="x86_64",
        )
        self.assertEqual((native, "native"), selected)
        fallback = select_asset(
            VERSION,
            {wheel: "b" * 64},
            system="Linux",
            machine="x86_64",
        )
        self.assertEqual((wheel, "wheel"), fallback)
        with self.assertRaises(ReleaseError):
            select_asset(
                "v0.1.1",
                {wheel: "b" * 64},
                system="Linux",
                machine="x86_64",
            )
        with self.assertRaises(ReleaseError):
            parse_checksums(f"{'a' * 64} asset\n{'a' * 64} asset\n")
        for invalid in ("v01.2.3", "v1.02.3", "v1.2.03"):
            with self.assertRaises(ReleaseError):
                native_asset_name(invalid, system="Linux", machine="x86_64")
        self.assertEqual(
            "worklease-v0.0.0-linux-x86_64",
            native_asset_name("v0.0.0", system="Linux", machine="x86_64"),
        )

    def test_checksum_verification_rejects_changed_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "asset"
            path.write_bytes(b"original")
            expected = hashlib.sha256(path.read_bytes()).hexdigest()
            verify_checksum(path, expected)
            path.write_bytes(b"tampered")
            with self.assertRaises(ReleaseError):
                verify_checksum(path, expected)

    def test_downloaded_native_artifact_is_installed_and_smoke_tested(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            release = root / "release"
            release.mkdir()
            native = release / native_asset_name(
                VERSION, system="Linux", machine="x86_64"
            )
            native.write_text(
                "#!/bin/sh\n"
                'printf \'%s\\n\' \'{"schemaVersion":1,"operation":"version",'
                '"ok":true,"version":"0.1.0"}\'\n',
                encoding="utf-8",
            )
            native.chmod(0o755)
            write_checksums(release)
            install_directory = root / "bin"
            result = self.run_installer(release, install_directory)
            self.assertEqual(0, result.returncode, result.stderr)
            payload = json.loads(result.stdout)
            self.assertEqual(native.name, payload["asset"])
            self.assertEqual("native", payload["kind"])
            self.assertTrue((install_directory / "worklease").is_file())

    def test_native_install_rejects_destination_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            release = root / "release"
            release.mkdir()
            native = release / native_asset_name(
                VERSION, system="Linux", machine="x86_64"
            )
            native.write_text(
                "#!/bin/sh\n"
                'printf \'%s\\n\' \'{"schemaVersion":1,"operation":"version",'
                '"ok":true,"version":"0.1.0"}\'\n',
                encoding="utf-8",
            )
            native.chmod(0o755)
            write_checksums(release)
            install_directory = root / "bin"
            install_directory.mkdir()
            destination = install_directory / "worklease"
            sentinel = root / "sentinel"
            sentinel.write_text("do not replace\n", encoding="utf-8")
            destination.symlink_to(sentinel)

            result = self.run_installer(release, install_directory)

            self.assertNotEqual(0, result.returncode)
            self.assertIn("not a regular file", result.stderr)
            self.assertEqual("do not replace\n", sentinel.read_text())

    def test_native_checksum_rejection_stops_install(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            release = root / "release"
            release.mkdir()
            native = release / native_asset_name(
                VERSION, system="Linux", machine="x86_64"
            )
            native.write_text("#!/bin/sh\nprintf bad\n", encoding="utf-8")
            native.chmod(0o755)
            write_checksums(release)
            native.write_text("#!/bin/sh\nprintf changed\n", encoding="utf-8")
            result = self.run_installer(release, root / "bin")
            self.assertNotEqual(0, result.returncode)
            self.assertIn("checksum mismatch", result.stderr)

    def test_wheel_fallback_verifies_and_smoke_tests_download(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            release = root / "release"
            release.mkdir()
            wheel = release / "worklease-0.1.0-py3-none-any.whl"
            wheel.write_bytes(b"wheel bytes")
            write_checksums(release)
            uv_log = root / "uv.log"
            fake_uv = root / "uv"
            fake_uv.write_text(
                "#!/bin/sh\n"
                "set -eu\n"
                'printf \'%s %s\\n\' "${UV_TOOL_BIN_DIR:-}" "$*" >> "$UV_LOG"\n'
                'if [ "$1" = tool ] && [ "$2" = install ]; then\n'
                "  cat > \"$UV_TOOL_BIN_DIR/managed-worklease\" <<'SCRIPT'\n"
                "#!/bin/sh\n"
                'printf \'%s\\n\' \'{"schemaVersion":1,"operation":"version","ok":true,"version":"0.1.0"}\'\n'
                "SCRIPT\n"
                '  chmod +x "$UV_TOOL_BIN_DIR/managed-worklease"\n'
                '  ln -s managed-worklease "$UV_TOOL_BIN_DIR/worklease"\n'
                "fi\n"
                'if [ "$1" = tool ] && [ "$2" = run ]; then\n'
                '  printf \'%s\\n\' \'{"schemaVersion":1,"operation":"version",'
                '"ok":true,"version":"0.1.0"}\'\n'
                "fi\n",
                encoding="utf-8",
            )
            fake_uv.chmod(0o755)
            environment = os.environ.copy()
            environment["WORKLEASE_UV"] = str(fake_uv)
            environment["UV_LOG"] = str(uv_log)
            original = os.environ.copy()
            try:
                os.environ.update(environment)
                result = self.run_installer(release, root / "bin")
            finally:
                os.environ.clear()
                os.environ.update(original)
            self.assertEqual(0, result.returncode, result.stderr)
            payload = json.loads(result.stdout)
            self.assertEqual("wheel", payload["kind"])
            commands = uv_log.read_text(encoding="utf-8").splitlines()
            self.assertTrue(any(" tool install " in line for line in commands))
            self.assertTrue(any(" tool run " in line for line in commands))
            self.assertTrue(
                any(
                    str(root / "bin") in line
                    for line in commands
                    if " tool install " in line
                )
            )

    def test_manifest_covers_every_asset(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            (directory / "wheel.whl").write_bytes(b"wheel")
            (directory / "native").write_bytes(b"native")
            write_checksums(directory)
            validate_checksums(directory)
            (directory / "native").write_bytes(b"changed")
            with self.assertRaises(ValueError):
                validate_checksums(directory)


if __name__ == "__main__":
    unittest.main()
