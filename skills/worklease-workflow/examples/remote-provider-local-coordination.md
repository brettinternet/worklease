# Example: Remote Provider with Local Coordination

Use this shape for Linear, Jira, GitHub Issues, or another remote provider when Worklease can exclude cooperating callers on one host but the provider mutation does not share the Worklease fence.

## Composition

1. The caller resolves the provider, source, and item and reads the authoritative current version.
2. The source adapter normalizes `Source`, `WorkRef`, dependencies, and `WorkItem` state.
3. The generic workflow selects eligible work.
4. The adapter derives a deterministic item resource and acquires a coordination-only ownership epoch.
5. Immediately before mutation, refresh dependencies, the exact claim, and provider state.
6. Perform the caller-authorized provider mutation.
7. Re-read the provider item and verify the requested checkpoint.
8. Release only after verification.

```python
from worklease.adapters import key
from worklease.models import AcquireRequest
from worklease.store import LeaseStore

resource_key = key(provider_kind, source_locator, item_id, coordination_only=True)
assert resource_key.capability == "local-coordination"

store = LeaseStore()
acquired = store.acquire(
    AcquireRequest(
        resource=resource_key.resource,
        claim_id=claim_id,
        agent_id=agent_id,
        session_id=session_id,
        owner_id=owner_id,
        work_key=work_key,
        coordination_only=True,
    )
)
```

The provider integration then performs its own authorized refresh and write. A provider update can be successful and still remain unfenced:

```text
before = provider.readItem(ref)
claim = worklease.readClaim(resource)
provider.update(ref, requestedPatch)
after = provider.readItem(ref)
verify(after, requestedCheckpoint)
```

Record:

```text
guarantee: local-coordination
guaranteeScope: cooperating callers sharing this Worklease store on one host
providerMutationFenced: false
```

The pre/post reads can reveal a changed provider version or incorrect result, but they do not exclude another host or direct provider writer. On a version mismatch, ownership loss, permission error, or ambiguous response, return `conflict`/`ambiguous` and do not release until the provider checkpoint is reconciled or the claim expires.

Assignment, provider status, a comment, and local command success are visibility or evidence only; none replaces the Worklease claim or provider receipt.
