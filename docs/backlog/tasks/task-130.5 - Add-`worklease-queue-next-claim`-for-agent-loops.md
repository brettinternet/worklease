---
id: TASK-130.5
title: Add `worklease queue next --claim` for agent loops
status: To Do
assignee: []
created_date: '2026-09-23 16:43'
updated_date: '2026-09-23 16:44'
labels:
  - work-queue
  - authority
milestone: m-1
dependencies:
  - TASK-130.4
  - TASK-130.3
references:
  - internal/cli/lease_commands.go
  - internal/handle/handle.go
  - internal/instructions/instructions.go
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-130
priority: high
type: feature
ordinal: 47000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Agents started together on one backlog race. Each picks an item from provider status, spends minutes reading and planning, and only then acquires. The loser discovers the conflict late. Marking the item In Progress sooner cannot fix this: provider status is not the lock (D5), and two agents that pick at the same moment still collide. D28 moves acquisition into selection: one command walks the `selectNext` order, acquires the first free candidate, and skips contended ones without waiting, so concurrent loops split the ready wave in milliseconds.

This is a worker-owned claim, not the queue-owned human Claim for me (TASK-130.1). It uses the caller's session and the same contextual handle as `worklease acquire`, and the caller heartbeats and releases it. Plain `queue next` stays non-acquiring.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease queue next --view NAME --claim --json` runs `selectNext` over one complete snapshot and, in that order, revalidates each candidate's prerequisite closure and eligibility, runs the TASK-130.3 identity gate, and acquires its exact resources with no wait
- [ ] #2 The claim uses the caller's session (`--session` or `WORKLEASE_SESSION_ID`) and the same handle, admission, TTL, and pending-request machinery as `worklease acquire`; later `worklease heartbeat`, `verify`, and `release` work on it unchanged, and the queue never renews it
- [ ] #3 A contended candidate is recorded with holder and expiry and the next candidate is tried. The first success returns the candidate (ref, resources, keyInputs, readiness evidence) plus the claim receipt and the skipped candidates
- [ ] #4 When contention exhausts the snapshot's ready candidates, the result is the structured `active-claims` no-work outcome; other no-work reasons match plain `queue next`. Nothing sleeps, waits, or re-enumerates in a loop
- [ ] #5 An uncertain acquire stops immediately, is reported as uncertain with its pending path, and no further candidate is tried. At most one claim is ever held by one invocation
- [ ] #6 Without `--claim`, the command never acquires, as before
- [ ] #7 A concurrency test starts N (at least 8) invocations against the same view with at least N ready items on both local and remote authorities; every invocation claims a distinct item, and none reports a false conflict or holds two claims
- [ ] #8 `worklease instructions loop`, docs/queue.md, and the worklease-workflow skill recommend `queue next --claim` as the loop's selection step
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D28) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
