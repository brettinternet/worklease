"""Backlog.md resource identity policy using repository-local paths."""

from __future__ import annotations

from pathlib import Path

from .protocol import (
    BaseAdapter,
    ResourceKey,
    build_key,
    local_resource,
    normalize_provider,
    require_identity,
)


class BacklogMdAdapter(BaseAdapter):
    """Derive helper-fenced item keys for a Backlog.md project."""

    provider = "backlog-md"

    def key(
        self, source: str, item: str, *, coordination_only: bool = False
    ) -> ResourceKey:
        provider = normalize_provider(self.provider)
        source_value, item_value = require_identity(source, item, provider=provider)
        resource = local_resource(provider, Path(source_value), item_value)
        return build_key(
            provider=provider,
            source=source_value,
            item=item_value,
            resource=resource,
            capability="item-claim",
            scope="item",
            coordination_only=coordination_only,
        )


def create_adapter(provider: str = "backlog-md") -> BacklogMdAdapter:
    if normalize_provider(provider) != "backlog-md":
        raise ValueError("BacklogMdAdapter only accepts provider 'backlog-md'")
    return BacklogMdAdapter()


def key(source: str, item: str, *, coordination_only: bool = False) -> ResourceKey:
    return BacklogMdAdapter().key(source, item, coordination_only=coordination_only)


def resource_key(
    source: str, item: str, *, coordination_only: bool = False
) -> ResourceKey:
    return key(source, item, coordination_only=coordination_only)
