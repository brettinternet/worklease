#!/usr/bin/env python3
"""Create and validate a complete SHA-256 manifest for release assets."""

from __future__ import annotations

import argparse
import hashlib
import sys
import tarfile
import zipfile
from pathlib import Path

try:
    from .release_installer import parse_checksums
except ImportError:  # pragma: no cover - direct script execution
    from release_installer import parse_checksums


PACKAGE_DATA = (
    "worklease/py.typed",
    "worklease/schemas/v1/index.json",
    "worklease/schemas/v1/commands.json",
)
NATIVE_ARCHIVE_MEMBER = "bin/worklease"


def _missing_package_data(names: set[str], *, source_prefix: str = "") -> list[str]:
    """Return required public type/schema files absent from an artifact."""
    return sorted(
        required
        for required in PACKAGE_DATA
        if f"{source_prefix}{required}" not in names
    )


def validate_editable_package(package_directory: Path) -> None:
    """Validate public type/schema files in an editable source checkout."""
    names = {
        f"worklease/{path.relative_to(package_directory).as_posix()}"
        for path in package_directory.rglob("*")
        if path.is_file()
    }
    missing = _missing_package_data(names)
    if missing:
        raise ValueError(f"editable package data missing: {missing}")


def validate_python_artifact(path: Path) -> None:
    """Validate public type/schema files in a wheel or source archive."""
    if path.name.endswith(".whl"):
        with zipfile.ZipFile(path) as archive:
            names = set(archive.namelist())
        missing = _missing_package_data(names)
    elif path.name.endswith((".tar.gz", ".tgz")):
        with tarfile.open(path, "r:gz") as archive:
            names = {member.name for member in archive.getmembers()}
        missing = sorted(
            required
            for required in PACKAGE_DATA
            if not any(name.endswith(f"src/{required}") for name in names)
        )
    else:
        raise ValueError(f"unsupported Python artifact: {path.name}")
    if missing:
        raise ValueError(f"{path.name} package data missing: {missing}")


def _validate_native_content(name: str, content: bytes) -> None:
    """Validate PyInstaller's embedded public type/schema files."""
    missing = [
        required for required in PACKAGE_DATA if required.encode("utf-8") not in content
    ]
    if missing:
        raise ValueError(f"{name} native package data missing: {missing}")


def validate_native_artifact(path: Path) -> None:
    """Validate a raw executable or a mise-compatible native archive."""
    if path.name.endswith(".tar.gz"):
        try:
            with tarfile.open(path, "r:gz") as archive:
                try:
                    member = archive.getmember(NATIVE_ARCHIVE_MEMBER)
                except KeyError as error:
                    raise ValueError(
                        f"{path.name} has no {NATIVE_ARCHIVE_MEMBER}"
                    ) from error
                if not member.isfile() or not member.mode & 0o111:
                    raise ValueError(
                        f"{path.name} {NATIVE_ARCHIVE_MEMBER} is not executable"
                    )
                stream = archive.extractfile(member)
                if stream is None:  # pragma: no cover - guarded by isfile
                    raise ValueError(f"cannot read {NATIVE_ARCHIVE_MEMBER}")
                content = stream.read()
        except tarfile.TarError as error:
            raise ValueError(f"invalid native archive {path.name}: {error}") from error
    else:
        content = path.read_bytes()
    _validate_native_content(path.name, content)


def package_native_artifact(executable: Path, archive: Path) -> None:
    """Package one executable at mise's autodetected ``bin/worklease`` path."""
    if executable.is_symlink() or not executable.is_file():
        raise ValueError(f"native executable is not a regular file: {executable}")
    validate_native_artifact(executable)
    archive.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive, "w:gz") as output:
        output.add(executable, arcname=NATIVE_ARCHIVE_MEMBER, recursive=False)
    validate_native_artifact(archive)


def sha256_file(path: Path) -> str:
    """Hash a release asset in bounded chunks."""
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def asset_paths(directory: Path) -> list[Path]:
    """Return every release asset except the manifest itself."""
    return sorted(
        path
        for path in directory.iterdir()
        if path.is_file() and path.name != "checksums.txt"
    )


def write_checksums(directory: Path) -> Path:
    """Write a deterministic manifest covering every asset in ``directory``."""
    assets = asset_paths(directory)
    if not assets:
        raise ValueError("release directory contains no assets")
    manifest = directory / "checksums.txt"
    manifest.write_text(
        "".join(f"{sha256_file(path)}  {path.name}\n" for path in assets),
        encoding="utf-8",
    )
    return manifest


def validate_checksums(directory: Path) -> None:
    """Require an exact, matching manifest for every release asset."""
    manifest = directory / "checksums.txt"
    if not manifest.is_file():
        raise ValueError("release directory has no checksums.txt")
    expected = parse_checksums(manifest.read_text(encoding="utf-8"))
    assets = asset_paths(directory)
    actual_names = {path.name for path in assets}
    if set(expected) != actual_names:
        missing = sorted(actual_names - set(expected))
        extra = sorted(set(expected) - actual_names)
        raise ValueError(
            f"checksum coverage mismatch: missing={missing}, extra={extra}"
        )
    for asset in assets:
        actual = sha256_file(asset)
        if actual != expected[asset.name]:
            raise ValueError(
                f"checksum mismatch for {asset.name}: expected {expected[asset.name]}, got {actual}"
            )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="operation", required=True)
    for operation in ("write", "verify"):
        command = commands.add_parser(operation)
        command.add_argument("--directory", type=Path, required=True)
    package_command = commands.add_parser(
        "verify-package",
        help="verify public type/schema files in one built artifact",
    )
    package_command.add_argument("--artifact", type=Path, required=True)
    package_command.add_argument(
        "--kind", choices=("editable", "python", "native"), required=True
    )
    native_command = commands.add_parser(
        "package-native",
        help="package one executable as a mise-compatible native archive",
    )
    native_command.add_argument("--executable", type=Path, required=True)
    native_command.add_argument("--archive", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        if args.operation == "write":
            manifest = write_checksums(args.directory)
            print(manifest)
        elif args.operation == "verify":
            validate_checksums(args.directory)
            print("checksums valid")
        elif args.operation == "package-native":
            package_native_artifact(args.executable, args.archive)
            print(args.archive)
        elif args.kind == "editable":
            validate_editable_package(args.artifact)
            print("editable package data valid")
        elif args.kind == "python":
            validate_python_artifact(args.artifact)
            print("Python package data valid")
        else:
            validate_native_artifact(args.artifact)
            print("native package data valid")
    except (OSError, ValueError) as error:
        print(f"release validation failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
