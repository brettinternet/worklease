---
id: TASK-129.5
title: Add the live claim overlay (D20)
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 21:25'
labels:
  - work-queue
  - authority
  - reviewed
milestone: m-1
dependencies:
  - TASK-128
references:
  - internal/watch
  - internal/authority/authority.go
  - internal/ledger
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-129
priority: high
type: feature
ordinal: 25000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-128.7 shows claim state from one-shot status reads. D20 and plan section 14 ("Claim overlay") specify the live version, which must never miss a change:
1. Read the head cursor with `Events("", 1)`.
2. Batch Status the visible and actionable resources, reusing the TASK-128.7 chunking.
3. Watch the namespace from that cursor. Events between the cursor and the snapshot replay harmlessly.
4. When an event touches a displayed resource, re-status just those resources.
5. Schedule a re-status at each displayed claim's authority-time expiresAt, because expiry appends no event.
6. On a history gap, discard the projection and restart from step 1. On a restore or authority change, also stop claim-dependent actions and follow the existing profile identity and recovery procedures before resubscribing. Never silently repin an authority or restore ID, and never treat rebuilding the snapshot as recovery for active handles.

Filtered watches accept at most 32 resources, so use one namespace watch per authority per client. Never use List or widen the filter limit.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The overlay implements steps 1-6 through the existing authority client with one namespace cursor watch per authority per process, and never calls List or filtered watches
- [x] #2 Expiry is re-checked at each displayed claim's authority-time expiresAt, with no dependence on the local wall clock
- [x] #3 On a history gap the projection is discarded and rebuilt, and the TUI shows the rebuild
- [x] #4 On a restore-ID or authority-ID change, claim-dependent actions stop, the existing profile identity and recovery procedures are required before resubscribing, and IDs are never silently repinned; the active queue-owned handle regression is staged in TASK-130.1
- [x] #5 Injected-race tests interleave acquire, renew, release, and expiry between the cursor read, the status snapshot, and the watch start, and assert that the final projection equals the authority's state in every interleaving
- [x] #6 Provider sync and claim freshness are tracked and displayed independently
- [x] #7 Both local and remote authorities are covered by tests. For remote, this includes an authority restart that produces a restore or gap
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend authority-backed claim projection with cursor-before-snapshot namespace watch, targeted status, authority-time expiry, gap rebuild, and identity gate.
2. Integrate independent claim/provider freshness and rebuilding state into queue/TUI.
3. Add race, local/remote restore and expiry tests; run focused and repository gates; commit, merge and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Partial implementation committed on task-129.5-overlay at 4a2c81e (not merged). Live namespace cursor watch, batched/targeted status, authority-clock expiry, gap rebuild, identity failure, TUI freshness, local race/retry tests implemented. Review found stalled-provider and transient-status/watch races; corrected and retested. mise run lint, format-check, test, typecheck, hooks all passed in worktree. Remaining: real remote authority restart producing restore/gap; active queue-owned handle fail-closed/recovery test (queue-owned handles are not yet implemented until TASK-130.1); TUI stalled-source live test and broader injected expiry interleavings. Keep In Progress. Next: resume existing worktree/branch, add remote and active-handle integration tests, address defects, rerun gates, finalize, merge and clean up. Do not merge this partial branch as complete.

Resumed in existing worktree and merged current main, resolving queue index/TUI integration (6da971a, not merged to main). Fixed history-gap invalidation ordering: preserve observation timestamp so a previously observed active claim becomes unknown during rebuild. Added provider-stall/claim-freshness and gap regression tests; keyed live-watch restarts to resource identity as well as item identity. Focused queue/queueui/cli tests and mise run lint, format-check, test, typecheck, hooks passed. No second general review pass; prior review findings remain addressed. Remaining: actual remote serve restart that produces restore/gap and active queue-owned handle fail-closed/recovery test. The latter needs TASK-130.1, which depends on this S3 parent (dependency cycle); decide to stage this acceptance test with TASK-130.1 or alter dependencies before marking complete. Keep In Progress and do not merge partial branch as complete. Next: implement real remote restart coverage, then resolve handle-test dependency and finalize.

Remote hosted authority restart/restore integration test added on task-129.5-overlay (2db2d64): pinned HTTP profile observes initial claim state, hosted store rotates restore ID offline, server restarts, and overlay fails closed rather than repinning. Focused test and mise run lint, format-check, test, typecheck, hooks passed. No further general review (prior pass already completed). Branch remains unmerged; acceptance #4 still requires an active queue-owned handle, unavailable until TASK-130.1, which currently depends on S3 parent; resolve dependency/acceptance sequencing before completion. Next: test queue-owned handle after lifecycle implementation or agree to stage that criterion; then revalidate, finalize, integrate, and clean up. Existing unrelated primary checkout modifications left untouched.

User approved staging the active queue-owned handle regression in TASK-130.1 because the handle is introduced there; D20 safety semantics remain unchanged. Acceptance #4 now covers the overlay identity gate, with end-to-end active-handle coverage transferred to TASK-130.1.

Acceptance evidence: internal/queue/live_claims_test.go cursor/status/watch race matrix (acquire/renew/release/expiry), local authority replay, authority-time expiry without event, gap rebuild, and real hosted HTTP remote restart/restore with pinned profile; internal/queueui/model_test.go rebuilding state, stalled-provider/live-claim independence. After merging current main into task branch, mise run lint, format-check, test, typecheck, hooks all passed. Plan D20 staging clarification committed in a40dfe7; active-handle regression transferred to TASK-130.1 #9.

Post-completion review: fixed unknown-after-failed-expiry retry, gap invalidation surviving provider snapshots, and made race fixture cursor-dependent (6d4f1ab).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented live namespace claim overlay with cursor-before-status replay, targeted status, authority-time expiry, visible gap rebuild, and restore/authority identity fail-closed. Tests cover injected races, local replay, real hosted remote restore/restart, and independent claim/provider freshness. All repository gates passed; merged into main at f7d7aba. Active queue-owned handle restore regression is TASK-130.1 #9 by user decision.
<!-- SECTION:FINAL_SUMMARY:END -->
