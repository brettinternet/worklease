---
id: TASK-129.2
title: Add the per-quota request scheduler
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
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
- [ ] #1 A scheduler keyed by quota identity runs GitHub requests serially per host and account within the process and runs Backlog.md processes with at most 4 concurrent per source, in priority order: action check, selected detail, visible page, background
- [ ] #2 Superseded reads are cancelled, duplicate in-flight fetches are coalesced, and pending jobs are capped with a structured overload diagnostic
- [ ] #3 Capacity is reserved so that an action check never waits behind background hydration
- [ ] #4 Rate-limit state (retry-after, reset time, and secondary limits) is shared per quota identity and surfaced as `rate-limited` with a retry time, separate from offline
- [ ] #5 GitHub mutations, when S6 adds them, are spaced at least 1 s apart per quota identity. The scheduler exposes this spacing now, and tests cover it
- [ ] #6 Uncertain writes are never retried. The scheduler retries only operations marked safe
- [ ] #7 Documentation and diagnostics never claim cross-process quota coordination. Aggregate load from several partitions or processes sharing an account is handled with jitter and backoff, and TASK-129.6 measures it
- [ ] #8 Tests with a fake clock and fake adapters cover priority inversion, cancellation, coalescing, overload, and rate limiting
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
