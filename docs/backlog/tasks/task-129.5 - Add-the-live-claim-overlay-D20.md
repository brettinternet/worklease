---
id: TASK-129.5
title: Add the live claim overlay (D20)
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 20:50'
labels:
  - work-queue
  - authority
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
- [ ] #1 The overlay implements steps 1-6 through the existing authority client with one namespace cursor watch per authority per process, and never calls List or filtered watches
- [ ] #2 Expiry is re-checked at each displayed claim's authority-time expiresAt, with no dependence on the local wall clock
- [ ] #3 On a history gap the projection is discarded and rebuilt, and the TUI shows the rebuild
- [ ] #4 On a restore-ID or authority-ID change, claim-dependent actions stop, the existing profile identity and recovery procedures are required before resubscribing, and IDs are never silently repinned, as tested with an active queue-owned handle
- [ ] #5 Injected-race tests interleave acquire, renew, release, and expiry between the cursor read, the status snapshot, and the watch start, and assert that the final projection equals the authority's state in every interleaving
- [ ] #6 Provider sync and claim freshness are tracked and displayed independently
- [ ] #7 Both local and remote authorities are covered by tests. For remote, this includes an authority restart that produces a restore or gap
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
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
<!-- SECTION:NOTES:END -->
