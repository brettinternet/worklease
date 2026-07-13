#!/usr/bin/env python3
"""Create and validate a complete SHA-256 manifest for release assets."""

from __future__ import annotations

import argparse
import hashlib
import sys
from pathlib import Path

try:
    from .release_installer import parse_checksums
except ImportError:  # pragma: no cover - direct script execution
    from release_installer import parse_checksums


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
    args = parser.parse_args(argv)
    try:
        if args.operation == "write":
            manifest = write_checksums(args.directory)
            print(manifest)
        else:
            validate_checksums(args.directory)
            print("checksums valid")
    except (OSError, ValueError) as error:
        print(f"release validation failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
