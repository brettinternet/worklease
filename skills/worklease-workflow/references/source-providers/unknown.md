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

Unknown provider names do not use an implicit resource-policy fallback. The
caller must explicitly select the built-in `generic` policy when a
coordination-only identity is appropriate:

```sh
worklease key --provider generic --source "$source_locator" --item "$item_id" --coordination-only
```

The generic policy preserves the supplied source and item in a deterministic
item-scoped local-coordination identity. It does not validate provider
semantics, discover items, execute writes, or provide provider fencing. A
misspelled dedicated provider fails as `resource-policy-not-found` instead of
silently joining another ownership domain.

Unknown or custom providers must explicitly select the `generic` resource
policy when appropriate. Custom source adapters remain caller-owned
capabilities. Worklease does not ship a plugin SDK or a mechanism for installing
custom resource policies.

## Guarantee and extension rule

Set `providerMutationFenced: false`. Change it only after the custom adapter
demonstrates a provider conditional-write operation that rejects stale writers
atomically and returns evidence. Pre/post reads, local locks, timestamps,
assignments, and command success do not satisfy that requirement.

Complete
[`../source-provider-authoring-checklist.md`](../source-provider-authoring-checklist.md)
before adding a dedicated provider reference. Add only the provider-specific
delta; inherit all scheduling and claim-lifecycle rules from
`worklease-workflow`.
