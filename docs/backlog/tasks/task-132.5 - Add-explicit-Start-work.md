---
id: TASK-132.5
title: Add explicit Start work
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 21:46'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies:
  - TASK-132.2
  - TASK-132.3
  - TASK-132.4
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-132
priority: high
type: feature
ordinal: 41000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Humans usually want to claim an item and mark it In Progress in one gesture. Plan section 8 ("Explicit Start work") and D26 allow that only as an explicit composition with separately reported outcomes, never as a new claim primitive and never with claimed atomicity across systems. `c` keeps meaning Claim only. Start work lives in the command palette and detail actions.

The steps: preview; revalidate prerequisites, eligibility, and permissions, then acquire; refresh provider state and verify ownership again, then perform the mapped start transition through the TASK-132.1 journaled path; read back and report each step. Assignment stays a separate opt-in action.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The preview shows source, provider actor, authority and resources, the mapped start transition, and required fields. When no start mapping is supported, only Claim is offered and no status or label is invented
- [x] #2 Prerequisites, eligibility, and permissions are revalidated before acquiring. Contention or failed revalidation stops the action before any provider write, as tested with a contending CLI claim
- [x] #3 After acquiring, provider state is refreshed and ownership verified again before the transition runs through the TASK-132.1 pipeline. Start work never assigns
- [x] #4 Each step is reported as applied, rejected, not attempted, or unknown. A transition that fails before any effect shows "Claim acquired; status unchanged" and offers correction or cancellation when eligible
- [x] #5 An uncertain transition enters recovery and is neither retried nor rolled back. Resuming uses the same verified private handle and recorded intent, never a new claim or operation ID
- [x] #6 The claim is kept for the work session under the normal TASK-130.2 heartbeat lifecycle
- [x] #7 Tests cover contention, a missing start mapping, revalidation failure, a transition rejected before any effect, and an uncertain transition, for both adapters
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Compose a dedicated TUI Start work preview from the existing claim plan and configured provider start mapping, with actor and transition disclosed; retain Claim-only c. 2. On confirmation revalidate and acquire, then refresh/verify the held claim and execute the existing journaled provider write; surface separate claim and transition outcomes, recovery, cancellation, and normal renewal. 3. Exercise Backlog.md and unsupported GitHub mappings plus contention, stale eligibility, rejected and unknown transitions in focused tests; run repository quality gates, review, integrate and finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented Start work in 694c63820ae5e26c3346bc9596ce4e5432a424de, fast-forward integrated into main. TUI palette/detail preview shows actor, source, mapping, required status, exact claim resources, commit/hook effects; c remains claim only. Confirmation revalidates dependency closure, live binding/actor/mapping and provider eligibility before acquisition; post-claim refresh and handle verification precede journaled write. Separate outcomes retain claim on rejection/unknown; recovery and heartbeat use existing paths. Reviewer found binding drift and misleading effects; both corrected. Verification: TestQueueStartWork* (including contending CLI claim, mapping drift, prerequisite reopen, GitHub unsupported mapping, real Backlog write), TestStartWork* (preview and applied/rejected/unknown results); focused go test -race -count=3; mise run lint, format-check, test, typecheck, hooks passed; focused checks passed on integrated main. No D26 or proposal section 8 change needed.

Follow-up preclaim adapter Inspect added in 77b93f587cc7b82484b18e7e48787517b3bc6029 (rebased from 24c3fa1), integrated on main. A short-lived test claim expired under a loaded full suite; the existing write-controller fixture now moves its single private handle and runs the normal renewal lifecycle. Reverified focused go test -race -count=3 for Start work and write-controller behavior, all four mise gates plus staged hooks, and integrated-main focused tests. Both Worktrunk worktrees and their exact Herdr workspaces were removed. GitHub Issues still has no start mapping: real adapter rejects it and queue offers Claim only; Backlog exercises the mapped transition. Rejected/unknown transition reporting is exercised at the TUI model; the journal/recovery behavior is covered by the existing write pipeline tests.

Post-completion review (merge e8b178d, fix de5d5ea): (1) Start work checkpoints cleared the queue-owned local handle's HoldUntil, so the next lifecycle renewal failed with 'local hold deadline missing'; queue write checkpoints now preserve any admitted hold (renewal asserted in TestQueueStartWorkComposesClaimAndProviderTransition). (2) GitHub Projects Start work preview passed the invalid operation ID 'start-preview' and always failed; now uses a real ID (TestQueueStartWorkPreviewsGitHubProjectStatusBinding). Also fixed a load-sensitive 30s TTL in TestQueueNextStartOutcomes. No follow-up needed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added explicit TUI Start work as a separately reported claim then journaled Backlog transition, with pre/post-claim revalidation and recovery; GitHub Issues remains Claim only. Commits 694c638 and 77b93f5 merged into main; lint, format, tests, typecheck, hooks and focused race checks passed.
<!-- SECTION:FINAL_SUMMARY:END -->
