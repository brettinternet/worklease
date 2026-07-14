"""Reusable conformance checks for source-provider adapters.

The kit validates provider boundaries only. It never schedules work, acquires
claims, or substitutes a local receipt for provider-authoritative state.
"""

from __future__ import annotations

from collections.abc import Mapping, Sequence
from dataclasses import dataclass

from .models import (
    CONTRACT_VERSION,
    CapabilityResult,
    ProviderReceipt,
    ResourcePolicySelection,
    ReviewBoundary,
    Source,
    WorkItem,
)
from .provider import SourceProvider


@dataclass(frozen=True, slots=True)
class ProviderConformanceCase:
    """Provider fixture and expected capability behavior for one test run."""

    source: Source
    item: WorkItem
    discovered: tuple[WorkItem, ...]
    context: Mapping[str, object] | None = None
    authority: object | None = None
    work_key: str = "conformance"
    stale_version: str | None = "__stale_provider_version__"
    unsupported_operations: frozenset[str] = frozenset()
    ambiguous_operations: frozenset[str] = frozenset()
    secrets: tuple[str, ...] = ()
    expected_sources: tuple[Source, ...] | None = None

    def __post_init__(self) -> None:
        if not isinstance(self.discovered, tuple):
            object.__setattr__(self, "discovered", tuple(self.discovered))
        if self.context is None:
            object.__setattr__(self, "context", {})
        if not isinstance(self.secrets, tuple):
            object.__setattr__(self, "secrets", tuple(self.secrets))


@dataclass(frozen=True, slots=True)
class ProviderConformanceReport:
    """Stable check and failure names from one conformance run."""

    provider: str
    contract_version: int | None
    checks: tuple[str, ...]
    failures: tuple[str, ...]

    @property
    def passed(self) -> bool:
        return not self.failures

    def to_dict(self) -> dict[str, object]:
        return {
            "provider": self.provider,
            "contractVersion": self.contract_version,
            "checks": list(self.checks),
            "failures": list(self.failures),
            "passed": self.passed,
        }


def _source_identity(source: Source) -> tuple[str, str, str]:
    return source.id, source.kind, source.locator


def _redacted(value: object, secrets: Sequence[str]) -> bool:
    rendered = repr(value)
    return not any(secret and secret in rendered for secret in secrets)


def _receipt_ok(
    receipt: ProviderReceipt,
    case: ProviderConformanceCase,
    operation: str,
    *,
    expected_outcome: str = "confirmed",
) -> list[str]:
    failures: list[str] = []
    if receipt.source_id != case.source.id:
        failures.append(f"{operation}-receipt-source")
    if receipt.ref is not None and receipt.ref != case.item.ref:
        failures.append(f"{operation}-receipt-ref")
    if receipt.operation != operation:
        failures.append(f"{operation}-receipt-operation")
    if not receipt.durable_location:
        failures.append(f"{operation}-receipt-location")
    if not receipt.observed_state:
        failures.append(f"{operation}-receipt-state")
    if receipt.outcome != expected_outcome:
        failures.append(f"{operation}-receipt-outcome")
    if receipt.conditional_write and receipt.fencing_evidence is None:
        failures.append(f"{operation}-fencing-evidence")
    if not _redacted(receipt, case.secrets):
        failures.append("token-redaction")
    return failures


def _capability_ok(
    result: object,
    provider_kind: str,
    operation: str,
    *,
    supported: bool,
    secrets: Sequence[str],
) -> bool:
    if not isinstance(result, CapabilityResult):
        return False
    if result.operation != operation or result.provider_kind != provider_kind:
        return False
    if result.supported != supported:
        return False
    if not supported and not result.reason:
        return False
    return _redacted(result, secrets)


def run_provider_conformance(
    provider: SourceProvider,
    case: ProviderConformanceCase,
) -> ProviderConformanceReport:
    """Check source qualification, capabilities, versions, receipts, and redaction."""

    provider_kind = getattr(provider, "kind", "")
    contract_version = getattr(provider, "contract_version", None)
    checks: list[str] = []
    failures: list[str] = []
    secrets = case.secrets

    if not isinstance(provider_kind, str) or not provider_kind.strip():
        failures.append("provider-kind")
    else:
        checks.append("provider-kind")
    if contract_version != CONTRACT_VERSION:
        failures.append("contract-version")
    else:
        checks.append("contract-version")

    try:
        resolved = provider.resolve((case.source.locator,), case.context or {})
        if isinstance(resolved, CapabilityResult):
            failures.append("resolve-unsupported")
        elif not isinstance(resolved, tuple) or not resolved:
            failures.append("source-resolution")
        elif len({_source_identity(source) for source in resolved}) != len(resolved):
            failures.append("source-resolution-duplicates")
        elif case.expected_sources is not None and tuple(
            _source_identity(source) for source in resolved
        ) != tuple(_source_identity(source) for source in case.expected_sources):
            failures.append("source-resolution-order")
        elif not any(
            _source_identity(source) == _source_identity(case.source)
            for source in resolved
        ):
            failures.append("source-qualification")
        else:
            checks.append("source-qualification")

        discovered = provider.discover(case.source)
        if isinstance(discovered, CapabilityResult):
            failures.append("discover-unsupported")
        elif not isinstance(discovered, tuple):
            failures.append("complete-discovery")
        else:
            discovered_refs = {item.ref for item in discovered}
            expected_refs = tuple(item.ref for item in case.discovered)
            actual_refs = tuple(item.ref for item in discovered)
            if actual_refs != expected_refs:
                failures.append("complete-discovery")
            elif len(discovered_refs) != len(discovered):
                failures.append("discovery-duplicates")
            elif any(
                item.ref.source_id != case.source.id
                or any(
                    dependency.source_id != case.source.id
                    for dependency in item.dependencies
                )
                for item in discovered
            ):
                failures.append("source-qualified-items")
            elif any(
                dependency not in discovered_refs
                for item in discovered
                for dependency in item.dependencies
            ):
                failures.append("dependency-closure")
            elif case.item.ref not in discovered_refs:
                failures.append("fixture-item-missing")
            else:
                checks.extend(
                    (
                        "complete-discovery",
                        "source-qualified-items",
                        "dependency-closure",
                    )
                )

        current_version = case.item.provider_version

        read_result = provider.read_item(case.item.ref)
        if isinstance(read_result, WorkItem) and read_result.ref == case.item.ref:
            checks.append("authoritative-read")
        else:
            failures.append("authoritative-read")

        policy = provider.resource_policy(case.item.ref, case.work_key)
        if "resource-policy" in case.unsupported_operations:
            if _capability_ok(
                policy,
                provider_kind,
                "resource-policy",
                supported=False,
                secrets=secrets,
            ):
                checks.append("unsupported-capability:resource-policy")
            else:
                failures.append("unsupported-capability:resource-policy")
        elif isinstance(policy, ResourcePolicySelection):
            repeat_policy = provider.resource_policy(case.item.ref, case.work_key)
            if (
                not isinstance(repeat_policy, ResourcePolicySelection)
                or policy.resource != repeat_policy.resource
            ):
                failures.append("resource-policy-stability")
            elif not policy.resource:
                failures.append("resource-policy")
            else:
                checks.append("resource-policy")
        elif _capability_ok(
            policy,
            provider_kind,
            "resource-policy",
            supported=False,
            secrets=secrets,
        ):
            failures.append("resource-policy-unsupported")
        else:
            failures.append("resource-policy")

        receipts: list[ProviderReceipt] = []
        write_result = provider.write_state(
            case.item.ref,
            {"conformance": "write-state"},
            case.authority,
            current_version,
        )
        if "write-state" in case.unsupported_operations:
            if _capability_ok(
                write_result,
                provider_kind,
                "write-state",
                supported=False,
                secrets=secrets,
            ):
                checks.append("unsupported-capability:" + "write-state")
            else:
                failures.append("unsupported-capability:" + "write-state")
        elif isinstance(write_result, ProviderReceipt):
            receipts.append(write_result)
            if write_result.provider_version is not None:
                current_version = write_result.provider_version
            failures.extend(
                _receipt_ok(
                    write_result,
                    case,
                    "write-state",
                    expected_outcome=(
                        "ambiguous"
                        if "write-state" in case.ambiguous_operations
                        else "confirmed"
                    ),
                )
            )
            if "write-state" in case.ambiguous_operations:
                if write_result.outcome == "ambiguous":
                    checks.append("ambiguous-outcome")
                else:
                    failures.append("ambiguous-outcome")
            else:
                checks.append("write-receipt")
        else:
            failures.append("write-state")

        if (
            case.stale_version is not None
            and "write-state" not in case.unsupported_operations
        ):
            stale_result = provider.write_state(
                case.item.ref,
                {"conformance": "stale-version"},
                case.authority,
                case.stale_version,
            )
            if _capability_ok(
                stale_result,
                provider_kind,
                "write-state",
                supported=False,
                secrets=secrets,
            ):
                checks.append("stale-version-rejection")
            else:
                failures.append("stale-version-rejection")

        progress_result = provider.record_progress(
            case.item.ref,
            {"conformance": "record-progress"},
            case.authority,
            current_version,
        )
        if "record-progress" in case.unsupported_operations:
            if _capability_ok(
                progress_result,
                provider_kind,
                "record-progress",
                supported=False,
                secrets=secrets,
            ):
                checks.append("unsupported-capability:" + "record-progress")
            else:
                failures.append("unsupported-capability:" + "record-progress")
        elif isinstance(progress_result, ProviderReceipt):
            receipts.append(progress_result)
            if progress_result.provider_version is not None:
                current_version = progress_result.provider_version
            failures.extend(
                _receipt_ok(
                    progress_result,
                    case,
                    "record-progress",
                    expected_outcome=(
                        "ambiguous"
                        if "record-progress" in case.ambiguous_operations
                        else "confirmed"
                    ),
                )
            )
            if "record-progress" in case.ambiguous_operations:
                if progress_result.outcome == "ambiguous":
                    checks.append("ambiguous-outcome")
                else:
                    failures.append("ambiguous-outcome")
            else:
                checks.append("progress-receipt")
        else:
            failures.append("record-progress")

        review_result = provider.resolve_review_boundary(
            case.source, None, case.authority
        )
        archive_result = provider.archive(
            case.item.ref, case.authority, current_version
        )
        if "review-boundary" in case.unsupported_operations:
            if _capability_ok(
                review_result,
                provider_kind,
                "review-boundary",
                supported=False,
                secrets=secrets,
            ):
                checks.append("unsupported-capability:" + "review-boundary")
            else:
                failures.append("unsupported-capability:" + "review-boundary")
        elif (
            isinstance(review_result, ReviewBoundary)
            and review_result.source_id == case.source.id
            and case.item.ref.item_id in review_result.item_ids
        ):
            checks.append("review-boundary")
        else:
            failures.append("review-boundary")

        if "archive" in case.unsupported_operations:
            if _capability_ok(
                archive_result,
                provider_kind,
                "archive",
                supported=False,
                secrets=secrets,
            ):
                checks.append("unsupported-capability:" + "archive")
            else:
                failures.append("unsupported-capability:" + "archive")
        elif isinstance(archive_result, ProviderReceipt):
            receipts.append(archive_result)
            if archive_result.provider_version is not None:
                current_version = archive_result.provider_version
            failures.extend(
                _receipt_ok(
                    archive_result,
                    case,
                    "archive",
                    expected_outcome=(
                        "ambiguous"
                        if "archive" in case.ambiguous_operations
                        else "confirmed"
                    ),
                )
            )
            if "archive" in case.ambiguous_operations:
                if archive_result.outcome == "ambiguous":
                    checks.append("ambiguous-outcome")
                else:
                    failures.append("ambiguous-outcome")
            else:
                checks.append("archive-receipt")
        elif not _redacted(archive_result, secrets):
            failures.append("token-redaction")
        else:
            failures.append("archive")

        if (
            isinstance(policy, ResourcePolicySelection)
            and policy.provider_fencing
            and (
                not receipts
                or any(
                    not receipt.conditional_write or receipt.fencing_evidence is None
                    for receipt in receipts
                )
            )
        ):
            failures.append("truthful-fencing-declaration")
        for receipt in receipts:
            if (
                case.item.provider_version is not None
                and receipt.provider_version is None
            ):
                failures.append("receipt-provider-version")

    except Exception as error:  # Adapters must expose structured capability results.
        failures.append("provider-exception")
        if not _redacted(error, secrets):
            failures.append("token-redaction")

    return ProviderConformanceReport(
        provider_kind,
        contract_version if isinstance(contract_version, int) else None,
        tuple(dict.fromkeys(checks)),
        tuple(dict.fromkeys(failures)),
    )


def assert_provider_conformance(
    provider: SourceProvider,
    case: ProviderConformanceCase,
) -> ProviderConformanceReport:
    """Run checks and raise with stable failure names when a provider is invalid."""

    report = run_provider_conformance(provider, case)
    if not report.passed:
        raise AssertionError(", ".join(report.failures))
    return report


__all__ = [
    "ProviderConformanceCase",
    "ProviderConformanceReport",
    "assert_provider_conformance",
    "run_provider_conformance",
]
