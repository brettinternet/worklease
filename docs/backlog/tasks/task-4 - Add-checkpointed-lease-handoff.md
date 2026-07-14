---
id: TASK-4
title: Add checkpointed lease handoff
status: In Progress
assignee:
  - '@brett'
created_date: '2026-07-14 02:06'
updated_date: '2026-07-14 03:53'
labels:
  - coordination
  - lease
dependencies: []
references:
  - src/worklease/models.py
  - src/worklease/store.py
  - src/worklease/execution.py
  - 'https://github.com/aetomala/worklease'
priority: high
type: feature
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add an optional, bounded checkpoint to the lease lifecycle so long-running work can resume safely after clean handoff or lease expiry. The checkpoint is coordination metadata only: the caller and provider remain authoritative for real work state, and this feature must not claim provider-side fencing or exactly-once external effects. A checkpoint write should renew ownership atomically and advance the claim revision.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 An active owner can write or replace a bounded checkpoint while atomically renewing the lease; stale, expired, and wrong-token callers are rejected without changing the checkpoint.
- [ ] #2 A subsequent acquire receives the last checkpoint plus an explicit clean-handoff versus expired-recovery indication, while read-only output never exposes bearer tokens.
- [ ] #3 Checkpoint updates, clean release, lease expiry, re-acquisition, stale-owner rejection, size limits, and idempotent retry behavior are covered by automated tests.
- [x] #4 The Python API, CLI JSON schema, and README/workflow documentation define checkpoint size, serialization, retention, and the unchanged local-coordination guarantee.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add bounded checkpoint validation and persistence to the lease model/store, including atomic renewal and acquire/recovery metadata. 2. Expose checkpoint through the CLI and public API with idempotent ownership checks. 3. Add focused lifecycle tests for update, replay, stale/expiry rejection, release/reacquisition, size and token redaction. 4. Leave documentation acceptance work for the next implementation task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation checkpoint (bounded-checkpoint-api): commit 138761207585dd5e5446e1d7aec81258cb7b075a adds 8 KiB canonical JSON checkpoints, SQLite migration/persistence across claims and releases, atomic checkpoint renewal with revision advance, clean-handoff/expired-recovery acquire metadata, CLI checkpoint command, token-redacted read-only projections, and lifecycle tests. mise run lint passed; mise run format-check passed (23 files); mise run test passed (62 tests); mise run typecheck passed (0 errors). Next task: document checkpoint size, JSON serialization, retention, CLI/API usage, and unchanged local-coordination guarantee in README/workflow docs. Remaining acceptance: #4 documentation; re-run full gates after docs.

Implementation checkpoint (checkpoint-documentation): commit 3efb6575f3e8eabbd7997bb08448829b675e323c documents the bounded checkpoint Python API, CLI version-1 JSON response, canonical JSON/UTF-8 serialization, 8 KiB limit, active/release/expiry retention and clean-handoff versus expired-recovery metadata, token redaction, and unchanged local-coordination guarantee in README.md and the Worklease Workflow guide; Python API docstrings now state the same contract. Verification: mise run lint passed; mise run format-check passed (23 files); mise run test passed (62 tests); mise run typecheck passed (0 errors); mise run hooks passed (ruff format, ruff check, 62 tests). Next task: review the complete accumulated TASK-4 implementation. Remaining acceptance: none; acceptance #4 checked; implementation tasks complete; next pass REVIEW.

Implementation follow-up (checkpoint-retention-policy): commit cb3e1850957c86ce06e5dc4bc31262c14d4f165f explicitly documents that checkpoint retention is not lease-TTL-limited and persists through clean release and expiry recovery until a future explicit retention/garbage-collection operation; README and Worklease Workflow guide now match the implementation. Verification: mise run hooks passed (62 tests); prior full gates remain green. Final implementation commits: 3efb6575f3e8eabbd7997bb08448829b675e323c and cb3e1850957c86ce06e5dc4bc31262c14d4f165f. Next task: review the complete accumulated TASK-4 implementation.

Implementation follow-up (checkpoint-token-replay-docs): commit 6b8dbbaa7cbe18a4b97b8d301a2a9954bc450181 documents that successful checkpoint mutation responses include the bearer token once, read-only status/list redact it, completed exact-operation replays may return cached receipts after expiry, and only changed same-operation requests or new/uncached stale/expired attempts fail; README and Worklease Workflow guide are aligned. Independent bounded acceptance review: README/workflow wording matches src/worklease/store.py replay ordering and src/worklease/models.py serialization; focused checkpoint lifecycle/size/recovery/redaction tests passed; mise run lint, mise run format-check, mise run test (62 tests), mise run typecheck, and mise run hooks all passed. Prior verifier findings were fixed and rechecked by direct contract inspection. Next task: REVIEW complete accumulated TASK-4 implementation.
<!-- SECTION:NOTES:END -->
