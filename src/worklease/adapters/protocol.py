"""Provider adapter contracts and deterministic resource identities.

Adapters deliberately stop at identity and capability policy.  They do not
import provider SDKs, perform network calls, or turn local leases into remote
fencing.  The core lease store remains provider-neutral.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol, runtime_checkable

from ..models import LeaseError

_PROVIDER_NAME = re.compile(r"^[a-z0-9-]+$")


@dataclass(frozen=True, slots=True)
class ResourceKey:
    """A stable local coordination identity and its claim capability."""

    provider: str
    source: str
    item: str
    resource: str
    capability: str
    scope: str
    fenced_mutations: bool
    provider_fencing: bool = False

    def to_dict(self) -> dict[str, object]:
        """Return the stable key response used by callers and the CLI."""

        return {
            "ok": True,
            "operation": "key",
            "provider": self.provider,
            "capability": self.capability,
            "scope": self.scope,
            "fencedMutations": self.fenced_mutations,
            "providerFencing": self.provider_fencing,
            "genericExecutionGuarantee": self.generic_execution_guarantee,
            "resource": self.resource,
        }

    @property
    def generic_execution_guarantee(self) -> str:
        """Generic local commands never imply provider-side fencing."""

        return "local-coordination"


@runtime_checkable
class ProviderAdapter(Protocol):
    """Minimal provider policy boundary implemented by bundled adapters."""

    provider: str

    def key(
        self, source: str, item: str, *, coordination_only: bool = False
    ) -> ResourceKey: ...

    @property
    def generic_execution_guarantee(self) -> str: ...

    def require_provider_fence(
        self, conditional_check: object | None = None
    ) -> None: ...


def normalize_provider(value: str) -> str:
    """Normalize and validate a provider name without accepting paths."""

    if not isinstance(value, str):
        raise LeaseError("invalid-provider", code=64, provider=value)
    provider = value.strip().lower()
    if not provider or _PROVIDER_NAME.fullmatch(provider) is None:
        raise LeaseError("invalid-provider", code=64, provider=value)
    return provider


def require_identity(source: str, item: str, *, provider: str) -> tuple[str, str]:
    """Validate caller-owned source and item identity while preserving content."""

    if not isinstance(source, str) or not source.strip():
        raise LeaseError(
            "invalid-resource-identity", code=64, provider=provider, field="source"
        )
    if not isinstance(item, str) or not item.strip():
        raise LeaseError(
            "invalid-resource-identity", code=64, provider=provider, field="item"
        )
    return source.strip(), item.strip()


def _git_output(cwd: Path, *arguments: str) -> str | None:
    """Read repository identity without importing a Git or provider library."""

    environment = os.environ.copy()
    # Git hooks export repository-scoped GIT_* variables for the main
    # checkout. Remove them so an isolated or temporary source resolves
    # against its own repository.
    for name in tuple(environment):
        if name.startswith("GIT_"):
            environment.pop(name, None)
    try:
        completed = subprocess.run(
            ["git", "-C", str(cwd), *arguments],
            text=True,
            capture_output=True,
            check=False,
            env=environment,
        )
    except OSError:
        return None
    if completed.returncode != 0:
        return None
    value = completed.stdout.strip()
    return value or None


def local_resource(provider: str, source_path: Path, item: str) -> str:
    """Match the reference helper's repository-aware local resource format."""

    source_path = source_path.expanduser().resolve()
    probe = source_path if source_path.is_dir() else source_path.parent
    top_value = _git_output(probe, "rev-parse", "--show-toplevel")
    common_value = _git_output(probe, "rev-parse", "--git-common-dir")
    if top_value and common_value:
        top = Path(top_value).resolve()
        common = Path(common_value)
        if not common.is_absolute():
            common = (probe / common).resolve()
        try:
            locator = source_path.relative_to(top).as_posix()
        except ValueError:
            locator = source_path.as_posix()
        authority = common.as_posix()
    else:
        authority = source_path.as_posix()
        locator = source_path.as_posix()
    target = "__source__" if provider == "markdown" else item
    return f"{provider}:{authority}:{locator}:{target}"


def coordination_resource(provider: str, source: str, item: str) -> str:
    """Derive a deterministic digest for providers without fencing."""

    locator = source.strip().rstrip("/")
    target = item.strip()
    if not locator or not target:
        raise LeaseError("invalid-resource-identity", code=64, provider=provider)
    identity = json.dumps(
        {"provider": provider, "source": locator, "item": target},
        sort_keys=True,
        separators=(",", ":"),
    )
    digest = hashlib.sha256(identity.encode("utf-8")).hexdigest()
    return f"coordination:{provider}:{digest}"


def build_key(
    *,
    provider: str,
    source: str,
    item: str,
    resource: str,
    capability: str,
    scope: str,
    coordination_only: bool,
) -> ResourceKey:
    """Construct a key with helper-compatible capability semantics."""

    return ResourceKey(
        provider=provider,
        source=source,
        item=item,
        resource=resource,
        capability=("local-coordination" if coordination_only else capability),
        scope=scope,
        fenced_mutations=not coordination_only and capability != "local-coordination",
    )


class BaseAdapter:
    """Common policy for adapters that do not own provider writes."""

    provider = ""
    claim_capability = "local-coordination"
    claim_scope = "item"

    def key(
        self, source: str, item: str, *, coordination_only: bool = False
    ) -> ResourceKey:
        raise NotImplementedError

    def require_provider_fence(self, conditional_check: object | None = None) -> None:
        """Reject provider-side work until an adapter owns a real CAS check."""

        del conditional_check
        raise LeaseError(
            "unsupported-provider-fencing",
            provider=self.provider,
            guarantee="local-coordination",
        )

    @property
    def generic_execution_guarantee(self) -> str:
        return "local-coordination"

    def provider_command(self, command: object) -> None:
        """Bundled adapters never execute provider commands themselves."""

        raise LeaseError(
            "unsupported-provider-exec",
            provider=self.provider,
            command=command,
        )
