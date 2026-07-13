# Unknown or Custom Provider

Use this reference only after the caller explicitly selects a provider kind for which no dedicated mapping exists. Unknown does not mean auto-detect or probe every available integration.

## Required declaration

Before scheduling, the caller must supply:

- one stable `Source.id` and source locator;
- complete discovery and source-qualified `WorkRef` mapping;
- terminal, blocked, dependency, priority, and order interpretation;
- authoritative item refresh and any permitted mutation operations;
- durable provider checkpoint and receipt semantics;
- review-boundary and archive capabilities or explicit `capability` outcomes; and
- credential and authority scope.

If any identity, read, resource, or required write capability is ambiguous, stop with `capability` or `ambiguous` before claiming work.

## Worklease resource policy

A syntactically valid custom provider name can use the bundled unknown-provider fallback:

```python
from worklease.adapters import key

resource_key = key(custom_provider_kind, source_locator, item_id)
```

The fallback preserves the provider name in deterministic resource identity and returns item-scoped `local-coordination`. It does not validate provider semantics, discover items, execute writes, or provide provider fencing.

A custom resource policy is allowed only when it documents canonical source/item identity, claim granularity, collision avoidance, and stability across worktrees and sessions. The exact resource remains opaque to the Worklease core.

## Guarantee and extension rule

Set `providerMutationFenced: false`. Change it only after the custom adapter demonstrates a provider conditional-write operation that rejects stale writers atomically and returns evidence. Pre/post reads, local locks, timestamps, assignments, and command success do not satisfy that requirement.

Complete [`../provider-authoring-checklist.md`](../provider-authoring-checklist.md) before replacing this fallback with a dedicated provider reference. Add only the provider-specific delta; inherit all scheduling and claim-lifecycle rules from `worklease-workflow`.
