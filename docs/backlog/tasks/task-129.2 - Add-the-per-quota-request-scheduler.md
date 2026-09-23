---
id: TASK-129.2
title: Add the per-quota request scheduler
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 20:10'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-128
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-129
priority: high
type: feature
ordinal: 22000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan section 14 ("Bounded work") needs one scheduler per process. It shares budgets per provider quota identity (for GitHub, host plus account), cancels superseded reads, deduplicates in-flight fetches, and reserves capacity for authoritative action checks. Priorities are action checks, then selected detail, then visible page, then background. Claim heartbeats must never wait behind it: they run on a separate control path (plan section 9, "Who renews a claim?"), which TASK-130.2 relies on.

This scheduler replaces the minimal per-account serialization added in TASK-128.5, and it also bounds Backlog.md subprocess concurrency (at most 4 processes per source).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A scheduler keyed by quota identity runs GitHub requests serially per host and account within the process and runs Backlog.md processes with at most 4 concurrent per source, in priority order: action check, selected detail, visible page, background
- [x] #2 Superseded reads are cancelled, duplicate in-flight fetches are coalesced, and pending jobs are capped with a structured overload diagnostic
- [x] #3 Capacity is reserved so that an action check never waits behind background hydration
- [x] #4 Rate-limit state (retry-after, reset time, and secondary limits) is shared per quota identity and surfaced as `rate-limited` with a retry time, separate from offline
- [x] #5 GitHub mutations, when S6 adds them, are spaced at least 1 s apart per quota identity. The scheduler exposes this spacing now, and tests cover it
- [x] #6 Uncertain writes are never retried. The scheduler retries only operations marked safe
- [x] #7 Documentation and diagnostics never claim cross-process quota coordination. Aggregate load from several partitions or processes sharing an account is handled with jitter and backoff, and TASK-129.6 measures it
- [x] #8 Tests with a fake clock and fake adapters cover priority inversion, cancellation, coalescing, overload, and rate limiting
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace the GitHub account mutex and Backlog subprocess slots with a process-wide quota-keyed priority scheduler; keep heartbeats outside it. 2. Add bounded pending work, cancellable/coalesced safe reads, shared rate-limit backoff and mutation spacing. 3. Integrate adapter request priorities and diagnostics; exercise fake-clock concurrency, cancellation, retries and quota tests. 4. Run repository gates, review, commit and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed under Worklease; implementing in task-129.2-scheduler worktree.

Integrated code commits 19ca5c0, 86b56b7, 30715cf into main (fast-forward). Focused scheduler and GitHub rate-limit tests, race checks, lint, format-check, full test, typecheck, and staged hooks passed. One review pass caught capacity inversion and active supersession; both corrected and rechecked. Claim heartbeats remain on their existing separate control path. Process-local quotas deliberately do not imply cross-process coordination.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added shared quota-keyed priority scheduling for GitHub reads and Backlog.md subprocesses, bounded coalesced/cancellable reads, action capacity, retry-time diagnostics and mutation spacing; verified with race tests and all required repository gates.
<!-- SECTION:FINAL_SUMMARY:END -->
