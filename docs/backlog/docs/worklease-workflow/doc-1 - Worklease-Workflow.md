---
id: doc-1
title: Worklease Workflow
type: guide
created_date: '2026-07-13 19:42'
updated_date: '2026-09-12 23:46'
tags:
  - agent
  - workflow
  - provider-neutral
---
# Worklease Workflow

Human-facing entry point for the provider-neutral coordination skill at [`skills/worklease-workflow/SKILL.md`](../../../../skills/worklease-workflow/SKILL.md).

## When to use it

Use the skill for dependency-aware selection, bounded ownership, heartbeats,
durable provider checkpoints, review boundaries, handoff, or archive. Continue
to use the provider's supported interface, such as the `backlog` CLI, for
provider reads and writes. The skill never edits provider files directly or
chooses a provider.

## Capability boundary

The caller supplies source resolution/discovery, item reads and durable writes,
dependency/status mapping, one to 32 exact ordered claim resources, claim
authority, provider receipts, and optional review/archive operations.

Worklease supplies a local SQLite claim authority and static deterministic key
policies. It does not discover provider work, authenticate to providers, perform
provider writes, or prove provider-side fencing. Source locators, IDs, statuses,
metadata, resources, and receipts stay opaque.

## Operating loop

1. Resolve ordered sources/selectors and discover the complete dependency graph.
2. Report complete, blocked, or active-claim outcomes when nothing is eligible.
3. Select terminal-prerequisite, unblocked work in provider order.
4. Acquire a fresh claim over the exact caller-supplied resource set before
   delegation, isolation, or edits.
5. Keep credentials only in an authority-bound private session handle or
   file/descriptor source; output never includes them.
6. Revalidate dependencies, ownership, guarantee scope, and provider state
   before every durable write.
7. Heartbeat before half the TTL and around bounded long operations.
8. Perform only caller-authorized provider mutations and retain/re-read their
   durable receipts.
9. Verify the authoritative provider checkpoint, persist bounded local recovery
   metadata, then release with an audit reason.
10. Review/archive only at an explicit authorized boundary.

Use stable distinct sessions for concurrent loops:

```sh
worklease acquire --resource "$RESOURCE" --session "$SESSION"
worklease verify --session "$SESSION"
worklease heartbeat --session "$SESSION"
worklease checkpoint --session "$SESSION" --data '{"phase":"verified"}'
worklease release --session "$SESSION" --reason "provider checkpoint verified"
```

One claim atomically covers one to 32 ordered resources. Credentials remain in
private handles or file/descriptor sources and never appear in output. Omit
operation IDs for ordinary mutations. Reuse one only for exact bounded replay
after a lost response. Changed intent conflicts. Unknown guarded outcomes
require provider-effect and process-cessation evidence plus explicit
reconciliation.

## Guarantees

Claim lifecycle and supervised exec provide `local-coordination` among
cooperating callers on one host. Exact expected-hash path replacement may report
`local-serialized-replace` for that file only. Provider mutations remain
`providerMutationFenced: false` unless the provider operation itself enforces a
conditional write/fence and returns evidence. Assignment, status, comments,
branches, worktrees, local locks, and receipts are not substitutes for claims or
provider checkpoints.

The local authority is the only shipped authority. Handles and event/watch
cursors bind to its immutable authority ID. The remote-authority design
document is explicitly deferred. Worklease has no HTTP backend or remote
fallback.

## References

Read [`references/contract.md`](../../../../skills/worklease-workflow/references/contract.md) first. If caller context does not already supply source capabilities, then read [`references/source-workflow.md`](../../../../skills/worklease-workflow/references/source-workflow.md) and only the matching provider reference.
