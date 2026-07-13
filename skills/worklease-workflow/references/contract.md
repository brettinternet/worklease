# Generic Work Coordination Contract

This contract defines a backend-neutral boundary for agents and human-operated tools that coordinate work. It does not define a provider, transport, database, filesystem layout, command, authorization mechanism, or source-specific status mapping.

## Design rule

The caller supplies capabilities and opaque values. The workflow must not infer a backend from an ID, path, title, environment variable, repository marker, or command result. If the caller cannot provide a required capability, return `capability` with the missing operation and stop that path.

## Normalized values

### `Source`

```text
Source {
  id: opaque stable source identity
  locator: opaque caller-resolved locator
  name: human-readable display name
  metadata: opaque caller-owned values
}
```

A source is an identity and capability context, not an authorization grant. Resolution is read-only until the caller passes explicit authority to a mutation.

### `WorkItem`

```text
WorkItem {
  source: Source
  id: opaque stable item identity
  title: string
  body: string or opaque caller-owned content
  dependencies: ordered opaque item IDs
  state: opaque caller state plus isTerminal/isBlocked booleans
  priority: opaque or null
  order: caller-defined stable ordering value or null
  progress: none | in-progress | complete
  review: none | pending | in-progress | complete
  claim: WorkClaim or null
  metadata: opaque caller-owned values
}
```

`state` remains caller-owned. The booleans are the only scheduling interpretation required by this contract: a dependency is implementation-ready only when the caller reports it terminal, and blocked work is never selected.

### `WorkClaim`

```text
WorkClaim {
  targetID: opaque item ID or source identity
  workKey: exact caller-defined operation key
  mode: opaque workflow mode
  claimID: unique ownership-epoch ID
  agentID: caller-visible agent identity
  ownerID: unique worker-attempt identity
  sessionID: invocation/session identity
  authority: opaque claim-authority identity
  guarantee: fenced | local-coordination | none
  revision: authority compare-and-set revision or opaque version
  token: opaque ownership token
  startedAt: authority timestamp
  heartbeatAt: authority timestamp
  expiresAt: authority timestamp
}
```

A claim is a bounded ownership epoch, not an in-progress marker. A new attempt may replace an expired claim with a fresh claim ID and token, but may not adopt or renew an unexpired claim because its agent or session identity matches. `fenced` means the caller's authority also guards the mutation it claims to guard. `local-coordination` means only cooperating callers under the stated local scope are excluded. `none` means no claim was acquired and no work may be delegated.

### `SchedulingScope`

```text
SchedulingScope {
  sources: ordered Source[]
  items: ordered WorkItem[]
  sourceOnly: boolean
  explicitItemIDs: ordered opaque IDs[]
  implementationItem: WorkItem or null
  reviewBoundary: ReviewBoundary or null
}
```

Explicit source and selector order is preserved. Dependency reads may include items outside an explicit selection, but selection and mutation remain inside the authorized scope. Source-only scope enumerates the complete collection; it is not a first-item shortcut and does not imply source-wide review.

### `ReviewBoundary`

```text
ReviewBoundary {
  id: opaque caller-defined ID or null
  label: string
  itemIDs: ordered opaque IDs[]
  explicit: boolean
}
```

The default review boundary is exactly one implementation item. A larger boundary requires an explicit caller request and a caller capability that can resolve and durably persist it. Never infer a milestone, parent, group, or source-wide review.

## Capability interface

A caller exposes equivalent operations. Names are illustrative; the caller may use another API if the observable guarantees remain the same.

1. `resolveSources(arguments, context)` returns ordered `Source[]` and diagnostics without mutating.
2. `discover(source, selector?)` returns every matching `WorkItem`, plus dependency items needed for graph validation.
3. `readItem(source, id)` refreshes one durable item before claim, write, review, or archive.
4. `selectNext(scope, mode)` returns one ordered claimable item or `complete`, `blocked`, or `active-claims`.
5. `selectWave(scope, mode)` returns only the dependency-ready, unclaimed wave available to the caller.
6. `claim(source, id, request, authority)` atomically claims an absent or expired target and returns the complete `WorkClaim` receipt.
7. `heartbeat(source, id, claimID, token, revision, authority)` conditionally extends only the matching unexpired claim.
8. `releaseClaim(source, id, claimID, token, revision, reason, authority)` conditionally releases only the matching claim after a durable checkpoint.
9. `writeState(source, id, patch, authority, claim?)` writes caller-owned durable state and returns a receipt.
10. `recordProgress(source, id, marker, authority, claim?)` writes the caller's durable progress checkpoint.
11. `reviewBoundary(scope, requestedBoundary?, authority)` resolves an explicit boundary or returns the one-item default.
12. `archive(source, target, authority, claim?)` performs an explicitly authorized caller-owned archive operation.

The caller passes authority and scope unchanged to mutations. Reads and resolution cannot expand either. A write receipt must identify the durable source/version where the caller can discover the result.

## Resolution and selection

- Resolve every explicit source and selector in supplied order. Do not reorder by priority, source type, or whichever capability answers first.
- If any explicit source fails, report that failure; do not silently derive another source.
- When no source is explicit, the caller may derive one only under its own unambiguous repository/context rules. This contract does not define those rules.
- Build the complete dependency graph before selecting work.
- Missing dependencies and directed cycles block every affected item.
- A dependency is not complete because it is assigned, claimed, in progress, reviewed, or locally marked; the caller must report it terminal.
- Exclude completed/terminal, blocked, dependency-ineligible, and actively claimed items.
- An unclaimed in-progress item may be resumed when the caller says it is eligible.
- If no work is selectable, report why: `complete`, `blocked`, `active-claims`, or a structured combination. Never select a later dependent merely to avoid an empty result.
- Within the ready wave, preserve explicit selector/source order, then apply only the caller's documented priority/order/stable-ID tie-breakers.

## Claim, lease, and mutation invariants

- Acquire with one compare-and-set operation, never a read-then-write marker.
- Generate globally unique claim, session, worker-attempt, and operation IDs.
- Retry the same operation ID only to recover the exact same request's lost response; changed inputs conflict.
- Every new ownership epoch uses a new claim ID and token. Retain the revision returned by the authority.
- Heartbeat and release require the exact current claim ID, token, and revision, plus a non-blank release reason.
- Use a bounded TTL accepted by the caller's authority. Heartbeat before half the lease elapses and around long work.
- Re-read eligibility and the exact claim immediately before durable mutation. After the mutation, re-read the claim and source/version when the caller can do so.
- If ownership or the write result is uncertain, stop further work and return an ambiguity/conflict diagnostic. Do not release a successor's claim.
- Persist a coherent task/progress/review/archive checkpoint through the caller before release. Handoff text alone is not durable state.
- A failed checkpoint keeps the work unreleased until repaired or expiry/recovery is handled by the caller.

## Authority guarantees

The caller must state the scope and guarantee it can prove:

- `fenced`: the claim authority and the guarded mutation share a compare-and-set/fencing boundary strong enough for the claimed operation.
- `local-coordination`: cooperating callers on the stated local scope are excluded, but the source mutation is not fenced by the claim. The caller must pre-check and post-check the claim and source around direct mutation.
- `none`: no usable ownership guarantee. Do not delegate or mutate as claimed work.

Never promote a lock, lease, assignment, status, comment, branch, worktree, or local cache into a stronger guarantee. Never describe local coordination as cross-host exclusion, source-side CAS, or mutation fencing.

## Durable authority and archive

The caller's backing source remains authoritative for item content, dependencies, status, progress, review, and completion. Any local claim store or cache is coordination/context only and is never a writable shadow source. Review and archive are caller operations, not scheduling shortcuts: they require explicit authority, matching claim where applicable, and a durable receipt. Deleting a source is not an archive operation unless the caller's own contract explicitly defines deletion as archive.

## Structured result vocabulary

A caller should make these outcomes machine-readable while preserving human-readable diagnostics:

- `complete`: all scoped work is terminal;
- `blocked`: unfinished work has missing/cyclic/blocked/unfinished prerequisites;
- `active-claims`: otherwise-ready work is held by unexpired claims;
- `capability`: a required caller operation or authority is unavailable;
- `ineligible`: the target fails a current readiness check;
- `conflict`: claim, revision, selector, or source version changed;
- `ambiguous`: the caller cannot establish what durable mutation occurred.

Unknown backend values belong in opaque metadata. They must not alter these coordination invariants.
