"""Reusable checks for resource-policy identity contracts.

The conformance helpers exercise only the provider-neutral resource policy
boundary. They do not acquire leases, discover provider items, or perform
provider writes.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

from ..models import LeaseError
from .protocol import ResourceKey
from .registry import (
    RESOURCE_POLICY_CONTRACT_VERSION,
    ResourcePolicyDescriptor,
    load_policy,
)


@dataclass(frozen=True, slots=True)
class PolicyConformanceReport:
    """Deterministic results from one policy conformance run."""

    provider: str
    descriptor: ResourcePolicyDescriptor | None
    checks: tuple[str, ...]
    failures: tuple[str, ...]

    @property
    def passed(self) -> bool:
        return not self.failures

    def to_dict(self) -> dict[str, object]:
        return {
            "provider": self.provider,
            "descriptor": (
                self.descriptor.to_dict() if self.descriptor is not None else None
            ),
            "checks": list(self.checks),
            "failures": list(self.failures),
            "passed": self.passed,
        }


def _key(registration: object, provider: str, source: str, item: str) -> ResourceKey:
    factory = getattr(registration, "factory", None)
    if not callable(factory):
        raise LeaseError("resource-policy-invalid-factory", provider=provider)
    adapter = factory(provider)
    key_method = getattr(adapter, "key", None)
    if not callable(key_method):
        raise LeaseError("resource-policy-invalid-factory", provider=provider)
    result = key_method(source, item)
    if not isinstance(result, ResourceKey):
        raise LeaseError("resource-policy-invalid-key", provider=provider)
    return result


def _cross_process_resource(
    provider: str, source: str, item: str, *, package_root: Path
) -> str:
    code = (
        "import json; from worklease.adapters import key; "
        f"print(json.dumps(key({provider!r}, {source!r}, {item!r}).resource))"
    )
    environment = os.environ.copy()
    source_root = package_root if package_root.name == "src" else package_root / "src"
    if source_root.is_dir():
        current = environment.get("PYTHONPATH")
        environment["PYTHONPATH"] = os.pathsep.join(
            value for value in (str(source_root), current) if value
        )
    completed = subprocess.run(
        [sys.executable, "-c", code],
        capture_output=True,
        text=True,
        check=False,
        env=environment,
    )
    if completed.returncode != 0:
        raise LeaseError("resource-policy-process-failed", provider=provider)
    try:
        value = json.loads(completed.stdout)
    except (TypeError, ValueError) as error:
        raise LeaseError(
            "resource-policy-process-invalid-output", provider=provider
        ) from error
    if not isinstance(value, str) or not value:
        raise LeaseError("resource-policy-process-invalid-output", provider=provider)
    return value


def run_policy_conformance(
    provider: str,
    *,
    source: str,
    items: Iterable[str] = ("item-a", "item-b"),
    equivalent_sources: Iterable[str] = (),
    expected_key_policy_version: int = 1,
) -> PolicyConformanceReport:
    """Check descriptor, collision, identity, and process stability invariants."""

    checks: list[str] = []
    failures: list[str] = []
    descriptor: ResourcePolicyDescriptor | None = None
    try:
        registration = load_policy(provider)
        descriptor = registration.descriptor
        if descriptor.contract_version != RESOURCE_POLICY_CONTRACT_VERSION:
            failures.append("contract-version")
        else:
            checks.append("contract-version")
        if descriptor.key_policy_version != expected_key_policy_version:
            failures.append("key-policy-version")
        else:
            checks.append("key-policy-version")

        item_values = tuple(items)
        if not item_values or len(set(item_values)) != len(item_values):
            failures.append("sample-items")
        else:
            resources = tuple(
                _key(registration, provider, source, item).resource
                for item in item_values
            )
            if descriptor.scope == "source":
                if len(set(resources)) != 1:
                    failures.append("scope-semantics")
                else:
                    checks.append("scope-semantics")
                other_source = _key(
                    registration,
                    provider,
                    f"{source}::conformance-other-source",
                    item_values[0],
                )
                if other_source.resource == resources[0]:
                    failures.append("collision-avoidance")
                else:
                    checks.append("collision-avoidance")
            elif len(set(resources)) != len(resources):
                failures.append("collision-avoidance")
            else:
                checks.append("collision-avoidance")
            repeated = tuple(
                _key(registration, provider, source, item).resource
                for item in item_values
            )
            if repeated != resources:
                failures.append("identity-stability")
            else:
                checks.append("identity-stability")
            package_root = Path(__file__).resolve().parents[2]
            process_resources = tuple(
                _cross_process_resource(
                    provider, source, item, package_root=package_root
                )
                for item in item_values
            )
            if process_resources != resources:
                failures.append("process-stability")
            else:
                checks.append("process-stability")
            for alternate in equivalent_sources:
                alternate_resources = tuple(
                    _key(registration, provider, alternate, item).resource
                    for item in item_values
                )
                if alternate_resources != resources:
                    failures.append("worktree-stability")
                    break
            else:
                checks.append("worktree-stability")
    except LeaseError as error:
        failures.append(error.reason)
    except Exception:
        failures.append("resource-policy-conformance-failed")
    return PolicyConformanceReport(provider, descriptor, tuple(checks), tuple(failures))


def assert_policy_conformance(
    provider: str,
    *,
    source: str,
    items: Iterable[str] = ("item-a", "item-b"),
    equivalent_sources: Iterable[str] = (),
    expected_key_policy_version: int = 1,
) -> PolicyConformanceReport:
    """Run conformance checks and raise with stable failure names when invalid."""

    report = run_policy_conformance(
        provider,
        source=source,
        items=items,
        equivalent_sources=equivalent_sources,
        expected_key_policy_version=expected_key_policy_version,
    )
    if not report.passed:
        raise AssertionError(", ".join(report.failures))
    return report


__all__ = [
    "PolicyConformanceReport",
    "assert_policy_conformance",
    "run_policy_conformance",
]
