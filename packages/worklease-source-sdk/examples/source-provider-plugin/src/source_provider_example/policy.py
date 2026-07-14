"""TASK-10 resource-policy composition for the example provider."""

from __future__ import annotations

from worklease.adapters import (
    ProviderAdapter,
    ResourceKey,
    ResourcePolicyDescriptor,
    ResourcePolicyRegistration,
)


class ExamplePolicy(ProviderAdapter):
    """Deterministic item policy; provider writes stay in the adapter."""

    provider = "example-source"
    claim_capability = "item-claim"
    claim_scope = "item"

    def key(
        self, source: str, item: str, *, coordination_only: bool = False
    ) -> ResourceKey:
        if not isinstance(source, str) or not source.strip():
            raise ValueError("source must be non-empty")
        if not isinstance(item, str) or not item.strip():
            raise ValueError("item must be non-empty")
        source = source.strip()
        item = item.strip()
        return ResourceKey(
            provider=self.provider,
            source=source,
            item=item,
            resource=f"{self.provider}:{source}#{item}",
            capability=(
                "local-coordination" if coordination_only else self.claim_capability
            ),
            scope=self.claim_scope,
            fenced_mutations=not coordination_only,
            provider_fencing=False,
        )

    def require_provider_fence(self, conditional_check: object | None = None) -> None:
        del conditional_check
        raise RuntimeError("example provider does not fence provider mutations")

    @property
    def generic_execution_guarantee(self) -> str:
        return "local-coordination"


registration = ResourcePolicyRegistration(
    ResourcePolicyDescriptor(
        name="example-source",
        origin="worklease-example-source-provider",
        origin_version="0.1.0",
        capability="item-claim",
        provider_fencing_supported=False,
    ),
    lambda _provider: ExamplePolicy(),
)
