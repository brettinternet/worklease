#!/usr/bin/env python3
"""Install an exact Worklease release without trusting a package index."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

_SHA256_RE = re.compile(r"^[0-9a-fA-F]{64}$")
_VERSION_RE = re.compile(r"^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$")
_REPOSITORY_RE = re.compile(r"^[^/\s]+/[^\s/]+$")


class ReleaseError(RuntimeError):
    """A release cannot be selected, verified, or installed safely."""


def package_version(version: str) -> str:
    """Return the package version for one exact ``vX.Y.Z`` tag."""
    if not _VERSION_RE.fullmatch(version):
        raise ReleaseError("VERSION must match vX.Y.Z exactly")
    return version[1:]


def native_asset_name(
    version: str, *, system: str | None = None, machine: str | None = None
) -> str:
    """Return the exact native asset name for a supported POSIX platform."""
    package_version(version)
    normalized_system = system or platform.system()
    normalized_machine = (machine or platform.machine()).lower()
    systems = {"Darwin": "macos", "Linux": "linux"}
    platform_name = systems.get(normalized_system)
    if platform_name is None:
        raise ReleaseError(f"unsupported operating system: {normalized_system}")
    architectures = {
        "amd64": "x64",
        "x86_64": "x64",
        "aarch64": "arm64",
        "arm64": "arm64",
    }
    architecture = architectures.get(normalized_machine)
    if architecture is None:
        raise ReleaseError(f"unsupported architecture: {normalized_machine}")
    return f"worklease-{version}-{platform_name}-{architecture}.tar.gz"


def wheel_asset_name(version: str) -> str:
    """Return the exact universal wheel asset name for one release tag."""
    return f"worklease-{package_version(version)}-py3-none-any.whl"


def parse_checksums(text: str) -> dict[str, str]:
    """Parse GNU-style SHA-256 lines and reject malformed or duplicate entries."""
    checksums: dict[str, str] = {}
    for line_number, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        fields = stripped.split()
        if len(fields) != 2 or not _SHA256_RE.fullmatch(fields[0]):
            raise ReleaseError(f"invalid checksum line {line_number}")
        digest, filename = fields[0].lower(), fields[1].lstrip("*")
        if not filename or filename in checksums:
            raise ReleaseError(f"duplicate checksum entry: {filename}")
        checksums[filename] = digest
    if not checksums:
        raise ReleaseError("checksums.txt contains no assets")
    return checksums


def select_asset(
    version: str,
    checksums: dict[str, str],
    *,
    system: str | None = None,
    machine: str | None = None,
) -> tuple[str, str]:
    """Select an exact native asset, otherwise the exact wheel fallback."""
    native = native_asset_name(version, system=system, machine=machine)
    if native in checksums:
        return native, "native"
    wheel = wheel_asset_name(version)
    if wheel not in checksums:
        raise ReleaseError(f"release has neither {native} nor {wheel}")
    return wheel, "wheel"


def sha256_file(path: Path) -> str:
    """Hash one downloaded asset in bounded chunks."""
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_checksum(path: Path, expected: str) -> None:
    """Reject an asset unless its bytes match the signed checksum manifest."""
    actual = sha256_file(path)
    if actual != expected.lower():
        raise ReleaseError(
            f"checksum mismatch for {path.name}: expected {expected}, got {actual}"
        )


def _download(url: str, destination: Path, *, missing_ok: bool = False) -> bool:
    request = Request(url, headers={"User-Agent": "worklease-release-installer"})
    try:
        with urlopen(request, timeout=30) as response, destination.open("wb") as stream:
            shutil.copyfileobj(response, stream)
    except HTTPError as error:
        if missing_ok and error.code == 404:
            return False
        raise ReleaseError(f"download failed for {url}: HTTP {error.code}") from error
    except (OSError, URLError) as error:
        raise ReleaseError(f"download failed for {url}: {error}") from error
    return True


def _version_from_output(output: str, expected: str) -> None:
    try:
        payload = json.loads(output)
    except json.JSONDecodeError as error:
        raise ReleaseError("downloaded worklease --version was not JSON") from error
    if not isinstance(payload, dict) or payload.get("version") != expected:
        raise ReleaseError("downloaded worklease reported the wrong version")
    if payload.get("schemaVersion") != 1 or payload.get("operation") != "version":
        raise ReleaseError("downloaded worklease returned an invalid version envelope")


def _smoke_native(executable: Path, expected: str) -> None:
    try:
        result = subprocess.run(
            [str(executable), "--json", "--version"],
            check=False,
            capture_output=True,
            text=True,
            timeout=30,
        )
    except OSError as error:
        raise ReleaseError(f"cannot execute {executable.name}: {error}") from error
    if result.returncode != 0:
        raise ReleaseError(
            f"downloaded {executable.name} --version failed with {result.returncode}"
        )
    _version_from_output(result.stdout.strip(), expected)


def _install_native_archive(archive: Path, install_directory: Path) -> Path:
    """Install only the expected executable from a verified native archive."""
    install_directory.mkdir(parents=True, exist_ok=True)
    target = install_directory / "worklease"
    if target.is_symlink() or (target.exists() and not target.is_file()):
        raise ReleaseError(f"install target is not a regular file: {target}")
    temporary_descriptor, temporary_name = tempfile.mkstemp(
        prefix=".worklease-", dir=install_directory
    )
    temporary = Path(temporary_name)
    try:
        with tarfile.open(archive, "r:gz") as source:
            try:
                member = source.getmember("bin/worklease")
            except KeyError as error:
                raise ReleaseError("native archive has no bin/worklease") from error
            if not member.isfile() or not member.mode & 0o111:
                raise ReleaseError("native archive bin/worklease is not executable")
            executable = source.extractfile(member)
            if executable is None:  # pragma: no cover - guarded by isfile
                raise ReleaseError("cannot read native archive bin/worklease")
            destination = os.fdopen(temporary_descriptor, "wb")
            temporary_descriptor = -1
            with destination:
                shutil.copyfileobj(executable, destination)
        temporary.chmod(0o755)
        os.replace(temporary, target)
    except (OSError, tarfile.TarError) as error:
        raise ReleaseError(f"cannot extract native archive: {error}") from error
    finally:
        if temporary_descriptor >= 0:
            os.close(temporary_descriptor)
        if temporary.exists():
            temporary.unlink()
    return target


def _smoke_wheel(uv: str, wheel: Path, expected: str) -> None:
    try:
        result = subprocess.run(
            [
                uv,
                "tool",
                "run",
                "--isolated",
                "--from",
                str(wheel),
                "worklease",
                "--json",
                "--version",
            ],
            check=False,
            capture_output=True,
            text=True,
            timeout=120,
        )
    except OSError as error:
        raise ReleaseError(f"cannot execute {uv}: {error}") from error
    if result.returncode != 0:
        raise ReleaseError(
            f"wheel smoke test failed with {result.returncode}: {result.stderr}"
        )
    _version_from_output(result.stdout.strip(), expected)


def _install_wheel(
    uv: str, wheel: Path, expected: str, install_directory: Path
) -> Path:
    install_directory.mkdir(parents=True, exist_ok=True)
    target = install_directory / "worklease"
    if target.is_symlink() or (target.exists() and not target.is_file()):
        raise ReleaseError(f"install target is not a regular file: {target}")
    environment = os.environ.copy()
    environment["UV_TOOL_BIN_DIR"] = str(install_directory)
    try:
        result = subprocess.run(
            [uv, "tool", "install", "--force", str(wheel)],
            check=False,
            capture_output=True,
            text=True,
            timeout=120,
            env=environment,
        )
    except OSError as error:
        raise ReleaseError(f"cannot execute {uv}: {error}") from error
    if result.returncode != 0:
        raise ReleaseError(f"wheel installation failed: {result.stderr}")
    _smoke_wheel(uv, wheel, expected)
    if target.is_symlink():
        try:
            source = target.resolve(strict=True)
        except OSError as error:
            raise ReleaseError(
                f"wheel install produced an unreadable executable: {target}"
            ) from error
    else:
        source = target
    if not source.is_file():
        raise ReleaseError(f"wheel install produced no executable: {target}")
    temporary_descriptor, temporary_name = tempfile.mkstemp(
        prefix=".worklease-", dir=install_directory
    )
    temporary = Path(temporary_name)
    try:
        os.close(temporary_descriptor)
        shutil.copyfile(source, temporary)
        temporary.chmod(0o755)
        os.replace(temporary, target)
    finally:
        if temporary.exists():
            temporary.unlink()
    _smoke_native(target, expected)
    return target


def _base_url(version: str) -> str:
    repository = os.environ.get("WORKLEASE_REPOSITORY", "")
    if not _REPOSITORY_RE.fullmatch(repository):
        raise ReleaseError("WORKLEASE_REPOSITORY must be owner/name")
    configured = os.environ.get("WORKLEASE_RELEASE_BASE_URL")
    if configured:
        return configured.rstrip("/") + "/"
    return f"https://github.com/{repository}/releases/download/{version}/"


def install_release(version: str, install_directory: Path) -> dict[str, str]:
    """Download, verify, install, and smoke-test one exact release."""
    expected_version = package_version(version)
    base_url = _base_url(version)
    with tempfile.TemporaryDirectory(prefix="worklease-release-") as temporary:
        directory = Path(temporary)
        checksums_path = directory / "checksums.txt"
        _download(f"{base_url}checksums.txt", checksums_path)
        checksums = parse_checksums(checksums_path.read_text(encoding="utf-8"))
        native = native_asset_name(
            version,
            system=os.environ.get("WORKLEASE_PLATFORM_SYSTEM"),
            machine=os.environ.get("WORKLEASE_PLATFORM_MACHINE"),
        )
        selected_name, kind = select_asset(
            version,
            checksums,
            system=os.environ.get("WORKLEASE_PLATFORM_SYSTEM"),
            machine=os.environ.get("WORKLEASE_PLATFORM_MACHINE"),
        )
        selected_path = directory / selected_name
        if kind == "native":
            downloaded = _download(
                f"{base_url}{native}", selected_path, missing_ok=True
            )
            if downloaded:
                verify_checksum(selected_path, checksums[selected_name])
                target = _install_native_archive(selected_path, install_directory)
                _smoke_native(target, expected_version)
                return {"asset": selected_name, "kind": kind, "path": str(target)}
        wheel_name = wheel_asset_name(version)
        if wheel_name not in checksums:
            raise ReleaseError(f"release fallback wheel is not listed: {wheel_name}")
        wheel_path = directory / wheel_name
        _download(f"{base_url}{wheel_name}", wheel_path)
        verify_checksum(wheel_path, checksums[wheel_name])
        installed = _install_wheel(
            os.environ.get("WORKLEASE_UV", "uv"),
            wheel_path,
            expected_version,
            install_directory,
        )
        return {"asset": wheel_name, "kind": "wheel", "path": str(installed)}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", nargs="?", default=os.environ.get("VERSION"))
    parser.add_argument(
        "--install-directory",
        default=os.environ.get("WORKLEASE_INSTALL_DIR", "~/.local/bin"),
    )
    args = parser.parse_args(argv)
    if not args.version:
        parser.error("VERSION or a vX.Y.Z argument is required")
    try:
        result = install_release(
            args.version, Path(args.install_directory).expanduser()
        )
    except ReleaseError as error:
        print(f"worklease release install failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps({"ok": True, "version": package_version(args.version), **result}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
