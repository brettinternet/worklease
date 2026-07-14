---
id: TASK-5
title: Add atomic multi-resource claim bundles
status: To Do
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:06'
updated_date: '2026-07-14 03:32'
labels:
  - coordination
  - leases
dependencies: []
references:
  - src/worklease/models.py
  - src/worklease/store.py
  - src/worklease/locking.py
  - 'https://github.com/simke9445/agentlocks'
  - 'https://github.com/ThatHunky/agent-coord'
priority: high
type: feature
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Allow a caller to acquire a finite set of exact opaque resources as one all-or-nothing claim bundle. This supports work that spans multiple files, source records, or related provider-local resources without partial ownership or lock-order deadlocks. Resource identity remains caller-supplied and opaque; glob expansion and provider discovery stay outside the core.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A bundle acquisition either owns every requested resource or owns none of them, including when one resource is already active or acquisition fails partway through.
- [ ] #2 Bundle acquisition uses deterministic ordering and same-host coordination so concurrent overlapping bundles cannot deadlock or leave partial claims behind.
- [ ] #3 The receipt provides a coherent way to inspect, heartbeat, execute guarded work against, and release the bundle; stale, expired, conflicting, and repeated requests have stable JSON errors and idempotent behavior.
- [ ] #4 Exact duplicate resources, empty bundles, oversized bundles, and invalid resource identities are rejected deterministically without leaking bearer tokens.
- [ ] #5 Automated concurrency tests and README/workflow documentation cover bundle semantics, conflict reporting, lifecycle behavior, and the unchanged provider-neutral guarantee.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Atomic bundle ownership (AC1, AC2, AC4).** Add bounded bundle request/result models and store operations that validate 1-32 exact non-empty resource strings, reject exact duplicates, take per-resource locks in deterministic lock-key order, and create one shared claim epoch/token/revision for all members in one SQLite transaction. Add failure-injection and overlapping-bundle tests proving all-or-none state and no deadlock.
2. **[T2] Bundle lifecycle and guarded operations (AC3, AC4).** Add inspect, heartbeat, guarded exec, and release operations that always authorize the complete ordered member set and update one bundle revision atomically. Wire schema-versioned CLI commands and deterministic idempotency/conflict errors; ensure read-only output and failures redact the shared token.
3. **[T3] Concurrency contract and documentation (AC5).** Add subprocess contention, expiry/reclaim, stale-owner, changed-replay, and partial-failure coverage. Document the 32-resource limit, exact identity/order semantics, whole-bundle lifecycle, and unchanged same-host/provider-neutral guarantee in README and workflow guidance.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now. No incomplete dependency, blocker, or human decision prevents implementation.

**Goal and target area:** Extend existing `src/worklease/models.py`, `store.py`, `locking.py`, `sqlite.py`, `cli.py`, and behavioral tests. No referenced local path is missing. External agentlocks/agent-coord links are design evidence only, not runtime dependencies.

**Resolved decisions:** A bundle contains 1-32 exact non-empty resource strings; input order is retained in the public receipt, while acquisition locks by deterministic internal lock-key order. Exact duplicates are errors. One bundle claim ID, bearer token, revision, expiry, and idempotency epoch authorize the complete member set; partial member lifecycle operations are invalid. All member claims and lifecycle revisions commit in one SQLite transaction. Read-only receipts omit tokens; successful owner acquire/heartbeat responses may return the one bundle token.

**Non-goals:** no glob expansion, provider discovery/network writes, distributed locks, cross-host/provider fencing, or independently releasable bundle members.

**Evidence and assumptions:** Existing single-resource validation, token/revision CAS, operation receipts, POSIX resource locks, and SQLite transactions are the pattern to share rather than duplicate. The 32-member limit bounds lock hold time and receipt size while covering intended multi-file/source use.

**Task/acceptance map:** T1→AC1/2/4; T2→AC3/4; T3→AC5.

**Pending verification:** targeted model/store/CLI tests, overlapping subprocess bundles, injected mid-acquire failure, expiry/reclaim, stale/idempotent replay, full quality gates.

**Next action:** implement T1 and keep the existing single-resource API behavior unchanged.

**Refinement checkpoint:** refined: TASK-5 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=a4c93723-9c76-467c-881f-b74aa78af792; claimRevision=3; refinement: complete.
<!-- SECTION:NOTES:END -->
