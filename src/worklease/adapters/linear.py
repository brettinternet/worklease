"""Coordination-only policy for Linear and future providers."""

from __future__ import annotations

from .protocol import (
    BaseAdapter,
    ResourceKey,
    build_key,
    coordination_resource,
    normalize_provider,
    require_identity,
)


class LinearAdapter(BaseAdapter):
    """Represent a provider without local mutation fencing."""

    def __init__(self, provider: str = "linear") -> None:
        self.provider = normalize_provider(provider)

    def key(
        self, source: str, item: str, *, coordination_only: bool = False
    ) -> ResourceKey:
        source_value, item_value = require_identity(
            source, item, provider=self.provider
        )
        resource = coordination_resource(self.provider, source_value, item_value)
        return build_key(
            provider=self.provider,
            source=source_value.rstrip("/"),
            item=item_value,
            resource=resource,
            capability="local-coordination",
            scope="item",
            coordination_only=True,
        )


def create_adapter(provider: str = "linear") -> LinearAdapter:
    return LinearAdapter(provider)


def key(source: str, item: str, *, coordination_only: bool = False) -> ResourceKey:
    return LinearAdapter().key(source, item, coordination_only=coordination_only)


def resource_key(
    source: str, item: str, *, coordination_only: bool = False
) -> ResourceKey:
    return key(source, item, coordination_only=coordination_only)
