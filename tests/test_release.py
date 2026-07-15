from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import subprocess
import tarfile
import tempfile
import tomllib
import unittest
from pathlib import Path

from scripts.embed_release_metadata import (
    render_release_metadata,
    write_release_metadata,
)
from scripts.release_artifacts import (
    NATIVE_ARCHIVE_MEMBER,
    PACKAGE_DATA,
    SDK_PACKAGE_DATA,
    package_native_artifact,
    validate_checksums,
    validate_editable_package,
    validate_native_artifact,
    validate_python_artifact,
    write_checksums,
)
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
            "worklease-v0.0.0-linux-x64.tar.gz",
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
            executable = root / "worklease"
            executable.write_text(
                "#!/bin/sh\n"
                'if [ "$1" != "--json" ] || [ "$2" != "--version" ]; then exit 91; fi\n'
                'printf \'%s\\n\' \'{"schemaVersion":1,"operation":"version",'
                '"ok":true,"version":"0.1.0"}\'\n'
                + "".join(f"# {path}\n" for path in PACKAGE_DATA),
                encoding="utf-8",
            )
            executable.chmod(0o755)
            package_native_artifact(executable, native)
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
            executable = root / "worklease"
            executable.write_text(
                "#!/bin/sh\n"
                'printf \'%s\\n\' \'{"schemaVersion":1,"operation":"version",'
                '"ok":true,"version":"0.1.0"}\'\n'
                + "".join(f"# {path}\n" for path in PACKAGE_DATA),
                encoding="utf-8",
            )
            executable.chmod(0o755)
            package_native_artifact(executable, native)
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
            executable = root / "worklease"
            executable.write_bytes(
                b"#!/bin/sh\nprintf bad\n\0"
                + b"\0".join(path.encode("utf-8") for path in PACKAGE_DATA)
            )
            executable.chmod(0o755)
            package_native_artifact(executable, native)
            write_checksums(release)
            native.write_bytes(b"changed")
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
                '  if [ "$7" != "--json" ] || [ "$8" != "--version" ]; then exit 91; fi\n'
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

    def test_release_metadata_generation_validates_and_embeds_version(self) -> None:
        self.assertIn(
            "PUBLISHED_RELEASE_VERSION: str | None = '0.4.0'",
            render_release_metadata("0.4.0"),
        )
        with self.assertRaises(ValueError):
            render_release_metadata("../unsafe")

        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary) / "_release_metadata.py"
            write_release_metadata("0.4.0", target)
            self.assertIn(
                "PUBLISHED_RELEASE_VERSION: str | None = '0.4.0'",
                target.read_text(encoding="utf-8"),
            )

    def test_release_toolchain_is_exact_and_locked(self) -> None:
        project = tomllib.loads((ROOT / "pyproject.toml").read_text())
        self.assertEqual(["hatchling==1.31.0"], project["build-system"]["requires"])
        self.assertEqual(
            ["hatchling==1.31.0", "pyinstaller==6.21.0"],
            project["dependency-groups"]["release"],
        )
        self.assertIn("release", project["tool"]["uv"]["default-groups"])

        locked = tomllib.loads((ROOT / "uv.lock").read_text())
        versions = {
            package["name"]: package["version"] for package in locked["package"]
        }
        self.assertEqual("1.31.0", versions["hatchling"])
        self.assertEqual("6.21.0", versions["pyinstaller"])

        workflows = "\n".join(
            (ROOT / path).read_text()
            for path in (".github/workflows/ci.yml", ".github/workflows/release.yml")
        )
        self.assertNotIn("--with pyinstaller", workflows)
        self.assertEqual(
            2, workflows.count("uv run --locked --group release pyinstaller")
        )
        self.assertIn("uv build --no-build-isolation", workflows)
        release = (ROOT / ".github/workflows/release.yml").read_text()
        self.assertEqual(2, release.count("scripts/embed_release_metadata.py"))
        self.assertEqual(4, release.count("expected=${RELEASE_TAG#v}"))
        self.assertIn("uv build --no-build-isolation", (ROOT / "mise.toml").read_text())

    def test_workflow_actions_and_permissions_are_immutable(self) -> None:
        action = re.compile(
            r"^\s*uses: [A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40} # v\d+\.\d+\.\d+$"
        )
        for name in ("ci.yml", "release.yml"):
            workflow = (ROOT / ".github/workflows" / name).read_text()
            uses = [line for line in workflow.splitlines() if "uses:" in line]
            self.assertTrue(uses)
            for line in uses:
                with self.subTest(workflow=name, action=line):
                    self.assertRegex(line, action)
            root = workflow.split("jobs:", 1)[0]
            self.assertIn("permissions:\n  contents: read", root)

        ci = (ROOT / ".github/workflows/ci.yml").read_text()
        release = (ROOT / ".github/workflows/release.yml").read_text()
        self.assertNotIn("contents: write", ci)
        self.assertEqual(1, release.count("contents: write"))
        publish = release.split("  publish:", 1)[1]
        self.assertIn("    permissions:\n      contents: write", publish)

    def test_package_artifacts_preserve_public_type_and_schema_data(self) -> None:
        validate_editable_package(ROOT / "src" / "worklease")
        uv = shutil.which("uv")
        self.assertIsNotNone(uv)
        assert uv is not None
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            result = subprocess.run(
                [
                    uv,
                    "build",
                    "--no-build-isolation",
                    "--out-dir",
                    str(output),
                ],
                cwd=ROOT,
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(0, result.returncode, result.stderr)
            artifacts = sorted(
                path
                for path in output.iterdir()
                if path.name.endswith((".whl", ".tar.gz"))
            )
            self.assertEqual(2, len(artifacts))
            self.assertEqual(1, sum(path.name.endswith(".whl") for path in artifacts))
            self.assertEqual(
                1, sum(path.name.endswith(".tar.gz") for path in artifacts)
            )
            for artifact in artifacts:
                validate_python_artifact(artifact)

            sdk_output = output / "sdk"
            sdk_result = subprocess.run(
                [
                    uv,
                    "build",
                    "--project",
                    str(ROOT / "packages/worklease-source-sdk"),
                    "--no-build-isolation",
                    "--out-dir",
                    str(sdk_output),
                ],
                cwd=ROOT,
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(0, sdk_result.returncode, sdk_result.stderr)
            sdk_artifacts = sorted(
                path
                for path in sdk_output.iterdir()
                if path.name.endswith((".whl", ".tar.gz"))
            )
            self.assertEqual(2, len(sdk_artifacts))
            for artifact in sdk_artifacts:
                validate_python_artifact(artifact, package_data=SDK_PACKAGE_DATA)
            native = output / "worklease-native"
            native.write_bytes(
                b"\0".join(path.encode("utf-8") for path in PACKAGE_DATA)
            )
            validate_native_artifact(native)
            native.write_bytes(PACKAGE_DATA[0].encode("utf-8"))
            with self.assertRaises(ValueError):
                validate_native_artifact(native)

            native.write_bytes(
                b"\0".join(path.encode("utf-8") for path in PACKAGE_DATA)
            )
            native.chmod(0o755)
            archive = output / "worklease-v0.1.0-linux-x64.tar.gz"
            package_native_artifact(native, archive)
            validate_native_artifact(archive)
            with tarfile.open(archive, "r:gz") as packaged:
                member = packaged.getmember(NATIVE_ARCHIVE_MEMBER)
                self.assertTrue(member.isfile())
                self.assertTrue(member.mode & 0o111)

    def test_workflow_packages_autodetected_native_archives(self) -> None:
        release = (ROOT / ".github/workflows/release.yml").read_text()
        self.assertIn("--project packages/worklease-source-sdk", release)
        self.assertIn("--kind sdk", release)
        for platform, architecture in (
            ("linux", "x64"),
            ("linux", "arm64"),
            ("macos", "x64"),
            ("macos", "arm64"),
        ):
            self.assertIn(f"platform: {platform}", release)
            self.assertIn(f"asset_arch: {architecture}", release)
        self.assertIn("${{ matrix.asset_arch }}.tar.gz", release)
        self.assertIn("package-native --executable dist/worklease", release)

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
