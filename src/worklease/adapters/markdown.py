"""Loose-Markdown source identity and expected-hash replacement bridge."""

from __future__ import annotations

import os
from pathlib import Path

from ..models import LeaseError, MutationRequest
from ..replacement import replace_file as _replace_file
from ..store import LeaseStore
from .protocol import (
    BaseAdapter,
    ResourceKey,
    build_key,
    local_resource,
    normalize_provider,
    require_identity,
)


class MarkdownAdapter(BaseAdapter):
    """Coordinate one Markdown source claim and delegate atomic replacement."""

    provider = "markdown"

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
            capability="source-claim",
            scope="source",
            coordination_only=coordination_only,
        )

    def source_resource(self, source: str | os.PathLike[str]) -> str:
        """Return the source-wide identity used by every Markdown item."""

        source_value = os.fspath(source)
        require_identity(source_value, "__source__", provider=self.provider)
        return local_resource(self.provider, Path(source_value), "")

    def replace_file(
        self,
        store: LeaseStore,
        request: MutationRequest,
        path: str | os.PathLike[str],
        expected_sha256: str,
        content_file: str | os.PathLike[str],
    ) -> dict[str, object]:
        """CAS-replace this claimed Markdown source through the T3 core."""

        target = Path(path).expanduser()
        if target.is_symlink():
            raise LeaseError("target-is-symlink", code=64, path=str(target))
        resolved_target = target.resolve(strict=False)
        expected_resource = self.source_resource(resolved_target)
        if request.resource != expected_resource:
            raise LeaseError(
                "resource-target-mismatch",
                resource=request.resource,
                expectedResource=expected_resource,
                path=str(resolved_target),
            )
        claim = store.validate_current(request)
        if claim.get("guarantee") != "fenced":
            raise LeaseError(
                "unsupported-coordination-replace-file",
                provider=self.provider,
            )
        return _replace_file(
            store,
            request,
            resolved_target,
            expected_sha256,
            content_file,
        )


def create_adapter(provider: str = "markdown") -> MarkdownAdapter:
    if normalize_provider(provider) != "markdown":
        raise ValueError("MarkdownAdapter only accepts provider 'markdown'")
    return MarkdownAdapter()


def key(source: str, item: str, *, coordination_only: bool = False) -> ResourceKey:
    return MarkdownAdapter().key(source, item, coordination_only=coordination_only)


def resource_key(
    source: str, item: str, *, coordination_only: bool = False
) -> ResourceKey:
    return key(source, item, coordination_only=coordination_only)


def replace_file(
    store: LeaseStore,
    request: MutationRequest,
    path: str | os.PathLike[str],
    expected_sha256: str,
    content_file: str | os.PathLike[str],
) -> dict[str, object]:
    """Module-level bridge for callers that do not retain an adapter object."""

    return MarkdownAdapter().replace_file(
        store, request, path, expected_sha256, content_file
    )
