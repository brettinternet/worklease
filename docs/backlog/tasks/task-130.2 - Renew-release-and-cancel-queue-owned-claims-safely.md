---
id: TASK-130.2
title: 'Renew, release, and cancel queue-owned claims safely'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - authority
milestone: m-1
dependencies:
  - TASK-130.1
references:
  - internal/handle/handle.go
  - internal/lease/service.go
  - skills/worklease-workflow/references/contract.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-130
priority: high
type: feature
ordinal: 30000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A queue-owned claim is renewed while that queue process runs: before half the TTL, with jitter and a safe deadline margin, on a control path that source refresh and indexing cannot delay (plan section 9, the TASK-129.2 scheduler). Suspend and resume, network uncertainty, and exit all need honest behavior. There is no renewal daemon and no promise of ownership after closing.

Release (`R`) needs a verified checkpoint per contract.md. The exception is the no-effect cancellation defined by TASK-126.3: a release with a non-completion reason, allowed only if the queue started no guarded operation or provider write under that claim.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Renewal runs on a dedicated control path before half the TTL, with jitter and a safety margin. The detail view shows the next renewal time and the last result
- [ ] #2 Under the TASK-129.6 combined-load scenario, renewal still meets its deadline margin (recorded)
- [ ] #3 After a suspend and resume or any network uncertainty, writes and further actions are disabled until ownership is verified against the authority, and a lost claim is shown as lost
- [ ] #4 Quitting with owned claims shows the exact consequence for each claim. The user can release when that is safe, or stop renewing and leave the remaining lease and recovery state visible. Exiting never releases an unresolved operation as completed
- [ ] #5 Release (`R`) requires a verified provider checkpoint, or else offers the TASK-126.3 cancellation when no guarded operation or provider write started under the claim. Cancellation is refused otherwise, and a test covers each case
- [ ] #6 Reopening the queue re-attaches owned claims only through the private handle plus live verification, never by a matching username
- [ ] #7 Tests cover renewal under a fake clock, suspend and resume, exit paths, release versus cancellation eligibility, and authority unavailability without fallback (D25)
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
