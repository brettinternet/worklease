"""Lazy provider adapter loading.

Importing :mod:`worklease.adapters` loads only the provider-neutral protocol.
Provider modules are imported on first use so optional policy stays outside the
core lease and execution modules.
"""

from __future__ import annotations

from collections.abc import Callable
from importlib import import_module
from typing import TYPE_CHECKING, cast

from ..models import LeaseError
from .protocol import ProviderAdapter, ResourceKey, normalize_provider

if TYPE_CHECKING:
    from types import ModuleType

_MODULES = {
    "github": ".github",
    "backlog-md": ".backlog_md",
    "markdown": ".markdown",
    "linear": ".linear",
}


def load_adapter(provider: str) -> ProviderAdapter:
    """Load exactly one adapter module on demand.

    Unknown provider names intentionally use Linear's coordination-only policy;
    they retain their provider name in the derived identity and never gain
    provider-fenced capabilities by accident.
    """

    normalized = normalize_provider(provider)
    module_name = _MODULES.get(normalized, ".linear")
    module: ModuleType = import_module(module_name, __name__)
    factory = getattr(module, "create_adapter", None)
    if not callable(factory):
        raise LeaseError("adapter-capability-unavailable", code=2, provider=normalized)
    factory = cast(Callable[[str], ProviderAdapter], factory)
    return factory(normalized)


def key(
    provider: str,
    source: str,
    item: str,
    *,
    coordination_only: bool = False,
) -> ResourceKey:
    """Derive and return one deterministic provider resource key."""

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
    return key(provider, source, item, coordination_only=coordination_only).to_dict()


__all__ = [
    "ProviderAdapter",
    "ResourceKey",
    "key",
    "key_result",
    "load_adapter",
    "resource_key",
]
