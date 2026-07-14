from __future__ import annotations

from worklease.adapters import (
    ResourceKey,
    ResourcePolicyDescriptor,
    ResourcePolicyRegistration,
)
from worklease.adapters.protocol import BaseAdapter, build_key, require_identity


class FixtureAdapter(BaseAdapter):
    provider = "fixture"
    claim_capability = "item-claim"
    claim_scope = "item"

    def key(
        self, source: str, item: str, *, coordination_only: bool = False
    ) -> ResourceKey:
        source, item = require_identity(source, item, provider=self.provider)
        return build_key(
            provider=self.provider,
            source=source,
            item=item,
            resource=f"fixture:{source}#{item}",
            capability=self.claim_capability,
            scope=self.claim_scope,
            coordination_only=coordination_only,
        )


registration = ResourcePolicyRegistration(
    ResourcePolicyDescriptor(
        name="fixture",
        origin="worklease-policy-fixture",
        origin_version="1.2.3",
        capability="item-claim",
    ),
    lambda provider: FixtureAdapter(),
)
