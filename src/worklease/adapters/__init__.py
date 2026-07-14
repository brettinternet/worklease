"""Lazy, versioned resource-policy loading.

Importing :mod:`worklease.adapters` loads only the provider-neutral protocol
and registry. Policy implementations and external entry points are imported
only after a caller selects one exact policy name.
"""

from __future__ import annotations

from .conformance import (
    PolicyConformanceReport,
    assert_policy_conformance,
    run_policy_conformance,
)
from .protocol import ProviderAdapter, ResourceKey
from .registry import (
    RESOURCE_POLICY_CONTRACT_VERSION,
    RESOURCE_POLICY_ENTRY_POINT_GROUP,
    ResourcePolicyDescriptor,
    ResourcePolicyRegistration,
    available_policy_names,
    describe_policy,
    load_adapter,
    load_policy,
    policy_descriptors,
)


def key(
    provider: str,
    source: str,
    item: str,
    *,
    coordination_only: bool = False,
) -> ResourceKey:
    """Derive one deterministic key using the selected resource policy."""

    return load_adapter(provider).key(source, item, coordination_only=coordination_only)


def resource_key(
    provider: str,
    source: str,
    item: str,
    *,
    coordination_only: bool = False,
) -> ResourceKey:
    """Alias matching the helper's operation name."""

    return key(provider, source, item, coordination_only=coordination_only)


def key_result(
    provider: str,
    source: str,
    item: str,
    *,
    coordination_only: bool = False,
) -> dict[str, object]:
    """Return the stable JSON-compatible key result."""

    return key(provider, source, item, coordination_only=coordination_only).to_dict()


__all__ = [
    "RESOURCE_POLICY_CONTRACT_VERSION",
    "RESOURCE_POLICY_ENTRY_POINT_GROUP",
    "ProviderAdapter",
    "ResourceKey",
    "ResourcePolicyDescriptor",
    "ResourcePolicyRegistration",
    "PolicyConformanceReport",
    "assert_policy_conformance",
    "available_policy_names",
    "describe_policy",
    "key",
    "key_result",
    "load_adapter",
    "load_policy",
    "policy_descriptors",
    "resource_key",
    "run_policy_conformance",
]
