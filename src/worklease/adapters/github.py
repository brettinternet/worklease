"""GitHub resource identity policy without importing GitHub clients."""

from __future__ import annotations

from .protocol import (
    BaseAdapter,
    ResourceKey,
    build_key,
    normalize_provider,
    require_identity,
)


class GitHubAdapter(BaseAdapter):
    """Derive helper-fenced item keys for GitHub issue work."""

    provider = "github"

    def key(
        self, source: str, item: str, *, coordination_only: bool = False
    ) -> ResourceKey:
        provider = normalize_provider(self.provider)
        source_value, item_value = require_identity(source, item, provider=provider)
        normalized_source = source_value.lower().removesuffix(".git")
        return build_key(
            provider=provider,
            source=normalized_source,
            item=item_value,
            resource=f"github:{normalized_source}#{item_value}",
            capability="item-claim",
            scope="item",
            coordination_only=coordination_only,
        )


def create_adapter(provider: str = "github") -> GitHubAdapter:
    if normalize_provider(provider) != "github":
        raise ValueError("GitHubAdapter only accepts provider 'github'")
    return GitHubAdapter()


def key(source: str, item: str, *, coordination_only: bool = False) -> ResourceKey:
    return GitHubAdapter().key(source, item, coordination_only=coordination_only)


def resource_key(
    source: str, item: str, *, coordination_only: bool = False
) -> ResourceKey:
    """Compatibility alias for callers that name the operation resource_key."""

    return key(source, item, coordination_only=coordination_only)
