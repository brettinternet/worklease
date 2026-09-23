---
id: TASK-132.5
title: Add explicit Start work
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
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
- [ ] #1 The preview shows source, provider actor, authority and resources, the mapped start transition, and required fields. When no start mapping is supported, only Claim is offered and no status or label is invented
- [ ] #2 Prerequisites, eligibility, and permissions are revalidated before acquiring. Contention or failed revalidation stops the action before any provider write, as tested with a contending CLI claim
- [ ] #3 After acquiring, provider state is refreshed and ownership verified again before the transition runs through the TASK-132.1 pipeline. Start work never assigns
- [ ] #4 Each step is reported as applied, rejected, not attempted, or unknown. A transition that fails before any effect shows "Claim acquired; status unchanged" and offers correction or cancellation when eligible
- [ ] #5 An uncertain transition enters recovery and is neither retried nor rolled back. Resuming uses the same verified private handle and recorded intent, never a new claim or operation ID
- [ ] #6 The claim is kept for the work session under the normal TASK-130.2 heartbeat lifecycle
- [ ] #7 Tests cover contention, a missing start mapping, revalidation failure, a transition rejected before any effect, and an uncertain transition, for both adapters
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
