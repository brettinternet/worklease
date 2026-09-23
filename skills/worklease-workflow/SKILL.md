---
name: worklease-workflow
description: Coordinate generic work items from Backlog.md, Markdown, GitHub Issues, Linear, Jira, or custom sources with dependency-aware selection, bounded claims, durable progress, review boundaries, and archive operations. Use when an agent needs a safe Worklease loop without assuming a backend or provider.
---

# Worklease Workflow

This is a provider-neutral coordination contract, not a provider integration.
The caller supplies source access, mutation authority, canonical resources, and
provider selection. IDs, statuses, metadata, and receipts remain opaque.

## Progressive loading

1. Read [`references/contract.md`](references/contract.md).
2. If source capabilities are not already supplied, read
   [`references/source-workflow.md`](references/source-workflow.md) and only the
   matching provider reference.
3. Declare source reads/writes, exact claim resources, claim authority,
   provider receipts, and review/archive capabilities.
4. Before claim lifecycle work, run the version-matched `worklease instructions
   safety`; run `worklease instructions loop` only for repeated autonomous
   loops. Installed guidance is authoritative for that installed version.
5. Use only caller-authorized capabilities. Missing capability is structured,
   never an invented fallback.

## Worklease boundary

Worklease can acquire, inspect, heartbeat, checkpoint, verify, and release one
claim over one to 32 exact ordered resources. It can supervise a local `exec` or
perform an expected-hash `replace-file`. It does not discover provider work,
interpret status/dependencies, execute provider writes, or establish
provider-side fencing.

The static built-in key policies may derive exact opaque resources after the
caller selects a provider, source, and item. They do not authenticate,
discover, mutate, or fence a provider. Exact resources contend by bytes within
one local authority namespace. Repository/path keys are host-local.

One claim ID, credential, revision, and expiry cover the complete resource set.
MCP leases have an absolute `maxHold` deadline; automatic renewal is bounded by
it. Any overlap conflicts, and acquisition is all-or-none.

## Normalized loop

1. Resolve sources/selectors in caller order and discover the complete required
   dependency graph.
2. Return `complete`, `blocked`, or `active-claims` when no item is eligible.
3. Evaluate each candidate's fresh, complete hard-prerequisite closure with the declared named condition per edge (legacy dependencies default to terminal). Known unsatisfied edges block even with other unknown edges; otherwise incomplete, stale, inaccessible, cyclic, or unsupported evidence stays unknown/capability. Select only ready, unblocked, claimable start/resume work in provider order. Hierarchy and related work are not hard edges; shared resources are claim contention.
4. Accept exact caller-supplied resources and acquire a fresh ownership epoch
   before delegation, isolation, or edits.
5. Retain non-secret claim metadata plus the private session handle. Never
   expose its credential.
6. Revalidate hard edges and completion evidence, ownership, guarantee scope, and provider state
   before every durable write.
7. Heartbeat before half the TTL and around bounded long-running operations.
8. Perform only caller-authorized provider writes and retain/re-read their
   durable receipts.
9. Checkpoint local recovery metadata only after the provider checkpoint is
   verified.
10. Review/archive only at explicit boundaries, then release the exact current
    claim with an audit reason. A verified current owner may report Blocked or
    record progress when prerequisites change, but cannot start/resume without
    readiness; every maintenance write still requires permission and a provider
    receipt, and completion requires its declared evidence.

Cancellation is a release with a non-completion reason, allowed only when no
guarded operation was started and no provider write was dispatched during that
ownership epoch. It never implies completion or creates a provider or Worklease
checkpoint, and reports the distinct `cancelled` outcome. Every other release
still requires a verified provider checkpoint; a started or unresolved/unknown
operation forbids cancellation.

A short CLI loop needs no credential plumbing:

```sh
worklease acquire --resource "$RESOURCE" --work-key "implement:TASK-42" --session "$SESSION"
worklease verify --session "$SESSION"
worklease heartbeat --session "$SESSION"
worklease checkpoint --session "$SESSION" --data '{"phase":"tests"}'
worklease release --session "$SESSION" --reason "provider checkpoint verified"
```

Give each independent loop one full, collision-resistant stable ID. Preserve a
harness workflow/loop-run ID unchanged; otherwise generate and persist a full
UUID. Never truncate or derive it from labels, agent names, turns, iterations,
or replacement sessions. Map it consistently to CLI `WORKLEASE_SESSION_ID` /
`--session`; for MCP, use `acquire.sessionId` and persist the returned opaque
lease. Use `--handle PATH` only when an explicit private handle is required.

Successful `verify` proves current access and ownership, not that this worker
created the claim; matching agent/session identity is insufficient to adopt an
existing claim.

Handles are authority-bound convenience state, never claims or provider
checkpoints. Acquire/transfer credentials are generated and persisted before
dispatch and never appear in output.

Explicit credentials are accepted only through `--token-file` or `--token-fd`,
never argv token text.

Omit `--operation-id` for a fresh mutation. Reuse one only to replay the exact
same pending request after a lost response. Changed intent conflicts.

A started guarded operation has an unknown outcome. Inspect the provider effect,
prove the old executor ceased, and use explicit CLI reconciliation before
retrying.

## Guarantees

Report only the demonstrated operation boundary:

| Situation | Reported guarantee |
| --- | --- |
| Matching local claim lifecycle or guarded `exec` among cooperating callers | `local-coordination` |
| Exact expected-hash `replace-file` with path membership | `local-serialized-replace` for that replacement |
| Durable provider mutation | `local-coordination` unless the provider itself returns conditional-write/fence evidence |

A provider CLI/API inside local exec does not become provider-fenced. Default
`providerMutationFenced` to false. Keep provider fencing evidence separate from
claim revision and event sequence.

## Safe failure

Return structured `blocked`, `active-claims`, `conflict`, `ambiguous`,
`ineligible`, `capability`, and `complete` results. Stop on uncertain canonical
resources, ownership, provider versions, receipts, or guarantee scope.
`stale-claim` means stop mutating. Credential failures require reloading the
private source and revalidating ownership.

Never expose credentials, exact private requests, command output, file contents,
checkpoints, reconciliation evidence, or provider payloads in status, events,
comments, logs, or handoffs. Handles and cursors must match the authority ID.
Expiry ends authorization but does not prove prior executor cessation. Before
resuming prior work, require explicit handoff or authoritative
abandonment/cessation evidence.

## Provider boundary

The source layer owns source detection, authentication, normalized state,
provider-specific writes, receipts, and review/archive behavior.

Worklease checkpoints are local recovery metadata, not provider progress. Verify
the authoritative provider receipt before checkpoint and release.

Assignment, status, comments, branches, worktrees, local locks, and operation
receipts are not substitutes for a claim or provider checkpoint.

## Experimental remote authority operations

The provider-neutral contract above is unchanged: the caller still supplies
source access, mutation authority, canonical resources, provider receipts, and
review/archive capabilities. A remote Worklease profile changes only the claim
authority location; it does not make Worklease a provider integration or grant
provider authority.

The experimental self-hosted remote authority is selected by explicit
`--profile NAME`, then `WORKLEASE_PROFILE`, user-side checkout binding, user
default, or local. `--local` is an explicit local override.

Configured remote failure never silently falls back. The standard binary opens no
listener and makes no network request unless remote profile management, a selected
remote profile, or `serve` is explicitly invoked. Local reads remain setup-free.

One remote namespace is one `serve` process and one SQLite writer on one host,
protected by the hosted lock.

Remote `--wait` is client-side (maximum 60s). Remote mutations retain durable exact pending requests, and remote same-host transfer requires a named predecessor handle.

Provider execution, provider cessation, and file replacement remain client-local.
`path`, `backlog-md`, and `markdown` keys are host-local and rejected by remote
admission.

See [`docs/remote-claim-authority.md`](../../docs/remote-claim-authority.md) for
experimental setup, operator restart, restore/reopen evidence, and unsupported
boundaries.

Never report remote claim ownership as provider fencing. On uncertain remote
mutation, retain the exact pending request, inspect/replay or reconcile it, and
establish executor/provider cessation before retrying.

Recovery import, completed-history journaling, cross-host transfer, repository enrollment, HA, Postgres, multi-namespace serving, backpressure, and browser control plane are not available.
