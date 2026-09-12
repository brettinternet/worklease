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
4. Use only caller-authorized capabilities. Missing capability is structured,
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
3. Select terminal-prerequisite, unblocked, claimable work in provider order.
4. Accept exact caller-supplied resources and acquire a fresh ownership epoch
   before delegation, isolation, or edits.
5. Retain non-secret claim metadata plus the private session handle. Never
   expose its credential.
6. Revalidate dependencies, ownership, guarantee scope, and provider state
   before every durable write.
7. Heartbeat before half the TTL and around bounded long-running operations.
8. Perform only caller-authorized provider writes and retain/re-read their
   durable receipts.
9. Checkpoint local recovery metadata only after the provider checkpoint is
   verified.
10. Review/archive only at explicit boundaries, then release the exact current
    claim with an audit reason.

A short CLI loop needs no credential plumbing:

```sh
worklease acquire --resource "$RESOURCE" --work-key "implement:TASK-42" --session "$SESSION"
worklease verify --session "$SESSION"
worklease heartbeat --session "$SESSION"
worklease checkpoint --session "$SESSION" --data '{"phase":"tests"}'
worklease release --session "$SESSION" --reason "provider checkpoint verified"
```

Use distinct `--session` values for concurrent loops in one checkout. Use
`--handle PATH` only when an explicit private handle is required. Handles are
authority-bound convenience state, never claims or provider checkpoints.
Acquire/transfer credentials are generated and persisted before dispatch and
never appear in output. Explicit credentials are accepted only through
`--token-file` or `--token-fd`, never argv token text.

Omit `--operation-id` for a fresh mutation. Reuse one only to replay the exact
same pending request after a lost response. Changed intent conflicts. A started
guarded operation has an unknown outcome; inspect the provider effect, prove the
old executor ceased, and use explicit CLI reconciliation before retrying.

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
Expiry ends ownership but does not prove an external process stopped.

## Provider boundary

The source layer owns source detection, authentication, normalized state,
provider-specific writes, receipts, and review/archive behavior. Worklease
checkpoints are local recovery metadata, not provider progress. Verify the
authoritative provider receipt before checkpoint and release. Assignment,
status, comments, branches, worktrees, local locks, and operation receipts are
not substitutes for a claim or provider checkpoint.

The local SQLite authority is the only shipped authority. The remote authority
proposal is deferred; never silently fall back from a configured remote service
or describe local coordination as cross-host exclusion.
