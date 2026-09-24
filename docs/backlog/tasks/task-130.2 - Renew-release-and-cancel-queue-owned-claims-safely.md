---
id: TASK-130.2
title: 'Renew, release, and cancel queue-owned claims safely'
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 12:34'
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
- [x] #1 Renewal runs on a dedicated control path before half the TTL, with jitter and a safety margin. The detail view shows the next renewal time and the last result
- [x] #2 Under the TASK-129.6 combined-load scenario, renewal still meets its deadline margin (recorded)
- [x] #3 After a suspend and resume or any network uncertainty, writes and further actions are disabled until ownership is verified against the authority, and a lost claim is shown as lost
- [x] #4 Quitting with owned claims shows the exact consequence for each claim. The user can release when that is safe, or stop renewing and leave the remaining lease and recovery state visible. Exiting never releases an unresolved operation as completed
- [x] #5 In S4, Release (`R`) offers TASK-126.3 no-effect cancellation only after live ownership and complete operation-history verification; it refuses claims with a guarded operation, unresolved request, or provider write. A Worklease checkpoint alone is not a verified provider checkpoint. Verified-provider-checkpoint release is delivered by S6 (TASK-132.1 and its provider adapters); tests cover allowed and refused S4 cancellation.
- [x] #6 Reopening the queue re-attaches owned claims only through the private handle plus live verification, never by a matching username
- [x] #7 Tests cover renewal under a fake clock, suspend and resume, exit paths, release versus cancellation eligibility, and authority unavailability without fallback (D25)
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add queue-owned renewal on an independent control path with a testable clock, deadline margin, and ownership verification after interruption. 2. Add safe release/cancellation and exit/reopen behavior using the persisted private handle and authoritative checks. 3. Exercise fake-clock, outage, suspend, exit, and combined-load tests; run gates, review once, integrate, and record evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Integrated ee38b83 into main. Dedicated per-handle control workers renew at 35–40% TTL with authority-time clock bounds, margin checks and persisted local hold; private handle/live verify gate reattach, R previews no-effect cancellation, exit preserves uncertain recovery. Reviewer identified remote deletion race, skew, serial verification, local hold and directory-error warning; corrected with tests. Full lint/format/test/typecheck/hooks pass. Opt-in combined control fixture (50k graph + 10k SQLite rows) renewed with 18.264819s remaining on prior 30s lease (target >=7.5s); TASK-129.6 PTY/rate-limit/stalled-HTTP fixture 1-sample measured local renewal 1.253458ms and margin 9m59.194983s at 10m TTL. Outstanding AC5 interpretation: S4 refuses release after an effect because S6 provider write/read-back pipeline does not yet exist; only no-effect cancellation is actionable.

User approved finalizing S4 with no-effect cancellation only; verified-provider-checkpoint release is deferred to S6 TASK-132.1, matching plan section 9. Reacquired same loop claim after a long user decision pause: prior epoch expired, reviewer and commands stopped, no active contender, provider task still eligible; recovery receipt retained.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Queue-owned claims now renew on an independent control path and fail closed on ownership uncertainty. R cancels only verified no-effect claims; exit/reopen preserve private recovery state. Integrated ee38b83 into main; local/remote lifecycle and UI tests, combined-load margin, full Go gates and hooks passed. Provider-checkpoint release remains S6 work by approved scope.
<!-- SECTION:FINAL_SUMMARY:END -->
