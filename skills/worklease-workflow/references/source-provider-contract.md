# Source Provider Contract

This contract defines the provider-specific delta required by the normative
[`contract.md`](contract.md). It does not replace or restate that contract.

## Adapter declaration

A source workflow adapter declares equivalent capabilities. Names are illustrative; the caller may use a CLI, SDK, MCP tool, database transaction, or local file operation when its observable behavior matches.

```text
SourceProvider {
  kind: opaque caller-selected provider kind
  resolve(arguments, context) -> ordered Source[] | diagnostic
  capabilities(source, principal, ref?, action?) -> CapabilitySet
  list(source, query, cursor, fields, budget) -> SummaryPage
  readItems(refs, fields, budget) -> per-reference outcomes
  readDependencies(ref, cursor, budget) -> typed edges + completeness
  changes(source, cursor, budget) -> Changes + nextCursor | unsupported # optional
  readItem(ref: WorkRef) -> current WorkItem plus provider version
  resourcePolicy(ref: WorkRef, workKey) -> exact resource plus local capability
  writeState(ref, patch, authority, expectedVersion?) -> ProviderReceipt
  recordProgress(ref, checkpoint, authority, expectedVersion?) -> ProviderReceipt
  resolveReviewBoundary(scope, explicitSelector, authority) -> ReviewBoundary
  archive(target, authority, expectedVersion?) -> ProviderReceipt
}
```

Resolution and discovery are read-only. Mutation operations exist only when the caller supplies explicit authority. Unsupported operations return `capability` with the missing operation and provider kind.

## Capabilities and response evidence

A `Capability` is a scoped semantic value, not a bare boolean:

```text
Capability {
  support: supported | unsupported | unknown
  permission: allowed | denied | unknown
  availability: available | unavailable | authentication-required
  semantics: operation-specific facts
  limits: operation-specific bounds
  reason: stable diagnostic when not actionable
}
```

Evaluate capabilities independently at adapter, source, principal, and
item/action scope. A capability at a wider scope does not imply one at a narrower
scope. Unknown never means allowed. Discovery is read-only and never probes a
capability by attempting a write.

Every adapter response carries common observation context, including responses
with per-item outcomes:

```text
ResponseContext {
  principal: opaque identity or null when unauthenticated
  configurationGeneration: opaque generation for relevant credentials/config
  observedAt: observation time
  coverage: complete | partial | unknown, with covered scope/cursor as needed
  providerVersion: opaque provider version or null when none exists
}
```

Keep four independent freshness/concurrency values distinct: Worklease claim
revision, provider version, provider update timestamp, and synchronization
cursor. A read ETag or timestamp never implies conditional-write capability.

## Queue-facing read semantics

Operation names below are illustrative, not a wire protocol. Equivalent
interfaces are valid when they preserve the same observable guarantees.

- Summary listing is paginated and returns a continuation cursor, coverage, and
  a total whose accuracy is explicitly `exact`, `estimated`, or `unknown`.
  Coverage states which requested scope is represented; a page is not a complete
  source snapshot merely because it has a cursor.
- Batched item reads return an outcome for every requested source-qualified
  reference (for example, found, missing/inaccessible, or failed). An adapter
  may use bounded individual reads when no batch endpoint exists, but never hide
  an unbounded one-process-per-item fan-out. Report batch/collection limits.
- Dependency reads return typed relationship evidence and explicit completeness
  (`complete`, `partial`, or `unknown`) for the requested closure/page.
- A change feed is optional. When present, it returns changes and a next cursor;
  its ordering, retention, and gap/reset behavior are declared.
- Each response includes `ResponseContext` above. Per-item results retain the
  context needed to explain their own coverage and observation.

Dependency evidence preserves provider fact and configured interpretation
side-by-side:

```text
DependencyEvidence {
  relationshipType: opaque provider type plus normalized category
  direction: prerequisite | dependent | non-blocking | unknown
  from: source-qualified WorkRef
  to: source-qualified WorkRef
  observedAt: observation time or null
  providerVersion: opaque version or null
  completionCondition: declared condition or unknown
  rawOutcome: opaque provider relationship/outcome
  interpretation: configured meaning and evidence used for this result
}
```

Do not infer a hard prerequisite from hierarchy or related-work links. Keep
unresolved, inaccessible, and unsupported relationships explicit. Resolving a
reference never authorizes a new source, endpoint, or credential scope.

## Normalized mapping

- `Source.id` is stable within caller context and qualifies every item reference.
- `WorkRef` is `{sourceID, itemID}`; provider-local IDs are never treated as globally unique.
- `WorkItem.ref` equals the exact source-qualified reference.
- `WorkItem.dependencies` contains complete source-qualified prerequisites, including unresolved references as blocking diagnostics.
- `state.isTerminal` and `state.isBlocked` are explicit adapter interpretations; raw provider status remains in metadata.
- Provider priority/order values are normalized without overriding dependency edges or explicit source/selector order.
- Provider versions, updated timestamps, ETags, or transaction revisions remain provider metadata or `ProviderReceipt` fields. They never replace the Worklease claim revision.

A provider reference must define how each value is read and how duplicate or ambiguous selectors fail. It must not define a competing scheduling algorithm.

The declared capability groups are Identity, Discovery, Dependencies, State,
Progress, Assignment, Native claims, Mutation, Synchronization, Effects, and
Authentication. Effects include non-item side effects such as Git fetch,
commit, hooks, and watcher notifications. Authentication declares credential
methods, origin/host scope, principal identity, and expiry/refresh semantics.

## Resource policy

The caller supplies provider, source, and item identity before resource
derivation. Prefer the static built-in Worklease key policy when its
identity and claim scope fit:

```sh
worklease --json key --provider "$provider_kind" --source "$source_locator" --item "$item_id"
```

The caller reads the returned envelope's `resource` and passes it unchanged to Worklease. `capability` and
`scope` describe local coordination policy, not provider discovery or
provider-side fencing. Every contender for the same logical claim scope must
receive the same exact resource.

Resource policies are separate from source-provider adapters: they only select
the local resource identity and guarantee declaration. A source-provider
adapter owns reads, writes, receipts, review boundaries, and archive behavior;
the generic workflow owns scheduling and claim lifecycle.

Supported policies are `backlog-md`, `markdown`, `github`, `linear`, `generic`,
and `path`.

Source adapters remain caller-owned capabilities. Worklease does not ship a
plugin SDK or a mechanism for installing custom resource policies. Use
`generic` explicitly for an unknown or custom provider when its resource
behavior matches the generic policy.

## Structured diagnostics

Adapters report structured diagnostics without replacing the generic workflow
result vocabulary in [`contract.md`](contract.md): `complete`, `blocked`,
`active-claims`, `capability`, `ineligible`, `conflict`, and `ambiguous` remain
the workflow outcomes. Diagnostics identify at least unsupported capability,
authentication required/failed, authorization denied, conflict, rate limiting
(with retry time when known), unavailable source, incomplete graph, and unknown
outcome. Map each diagnostic to the applicable workflow outcome; preserve
provider-specific codes and details as opaque diagnostic metadata. Do not turn
unknown or incomplete evidence into permission or readiness.

## Provider receipts

```text
ProviderReceipt {
  sourceID: exact Source.id
  ref: WorkRef or null for source-wide writes
  operation: caller-authorized mutation
  providerVersion: opaque post-write version or null
  durableLocation: provider-native locator
  observedState: fields proving the requested checkpoint
  conditionalWrite: boolean
  fencingEvidence: opaque provider evidence or null
}
```

A command exit status or Worklease receipt alone is not a provider receipt. If the provider mutation response lacks the resulting version/state, re-read the authoritative source and retain that result. An ambiguous write remains `ambiguous`; do not infer success from a local operation receipt.

## Guarantee declaration

Record two separate facts:

1. Worklease claim `guarantee` and `guaranteeScope`, describing the guarded local operation or local coordination boundary.
2. `providerMutationFenced`, describing whether the durable provider mutation itself shared a provider compare-and-set/fencing boundary.

`providerMutationFenced` defaults to `false`. Set it to `true` only when
`conditionalWrite` is true and `fencingEvidence` proves the provider rejected
stale writers as part of the same durable mutation.

Pre/post reads detect some conflicts but do not fence the mutation.

## Generic workflow handoff

After producing normalized sources, items, resource policy, and declared
capabilities, hand them to `worklease-workflow`.

The provider adapter responds to capability calls when invoked. It does not
expose a scheduler, work loop, `selectNext`, `selectWave`, claim lifecycle, or
release policy. The normative contract alone decides graph construction,
operation ordering, claim/revalidation timing, checkpoint-before-release, and
structured outcomes.
