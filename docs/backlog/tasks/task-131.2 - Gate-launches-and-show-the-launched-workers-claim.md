---
id: TASK-131.2
title: Gate launches and show the launched worker's claim
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 15:05'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-131.1
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-131
priority: high
type: feature
ordinal: 35000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Launch must be unavailable when the D11 authority check fails, and while the queue itself holds a claim on any of the item's resources, because the worker's own acquisition would then fail as already claimed (D18). The user can release or cancel (TASK-130.2) and then launch. That is two explicit steps, and another worker may acquire in between; the UI must say so. After launch, the queue shows the worker's claim once it appears in the overlay. It neither supervises nor retries the worker.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `x` opens a launch picker listing the configured actions, with a preview showing the argv after substitution, the cwd, the environment variable names (not values), and the authority
- [x] #2 Launch is disabled with reason `authority-mismatch` when the D11 check fails, and with reason `queue-holds-claim` while the queue holds any of the item's resources. The latter explanation includes the release-then-launch race
- [x] #3 queue query JSON reports launch availability and reasons for each action, matching the TUI
- [x] #4 After launch, the worker's claim appears in the overlay attributed to its own session, and the queue never renews, releases, or supervises it
- [x] #5 A reference launcher script in the repository consumes WORKLEASE_QUEUE_RESOURCES and WORKLEASE_QUEUE_AUTHORITY_ID, verifies the authority ID before acquiring, and acquires exactly the passed resources. An end-to-end test runs it for a GitHub item and for a Backlog.md item with a portable generic binding and asserts the same authority ID and resources as the queue
- [x] #6 A successful process start is reported as launched, never as coordinated work. The item shows a worker claim only once one appears in the overlay, as tested with a launcher that exits without claiming
- [x] #7 Tests cover each gate, the preview, and failure to start the process (reported with no claim side effects)
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reuse launch handoff and claim overlay; add shared per-action gate/preview for TUI and query with queue-owned claim exclusion. 2. Wire picker and explicit confirmation with start revalidation, without managing child claims. 3. Add reference launcher and end-to-end/failure tests for authority/resources. 4. Run focused and repository gates; commit in worktree, integrate into main, verify provider and release.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed under Worklease loop; implementing S5 launch gates and handoff in isolated worktree.

Implemented shared launch gating/preview, TUI picker and process handoff, query launch actions, worker authority-id command and reference launcher. Focused queue/CLI tests passing; adding end-to-end tests and full checks next.

Review found three concrete issues; fixed retired-key migration handoff with pre-launch identity gate, exact-ref preview confirmation, and asynchronous child reaping. Verified focused migration/launcher/UI tests and full lint, format-check, test, typecheck. Hooks and integration pending.

Delivered on main: 4050c17 (implementation) and 23e9f0c (unclaimed-launch/overlay acceptance), both worktrees removed. Evidence: TestLaunchGatesAndPublicPreview, TestLaunchHandoffIncludesRetiredBindingKeys, TestQueueQueryReportsEachLaunchActionAndItsGate, TestLaunchPickerPreviewsAndDoesNotInventWorkerClaim, TestSuccessfulLaunchDoesNotClaimUntilWorkerAppearsInOverlay, TestReferenceLauncherClaimsExactQueueHandoff (GitHub and portable Backlog binding, exact authority/resources, no-claim child), TestLaunchProcessStartFailureHasNoClaimSideEffect, TestDetachedLaunchReapsShortLivedChildren; mise run lint, format-check, test, typecheck, hooks all passed. One general review pass: three concrete findings corrected and retested; no remaining blocker.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added shared launch gates/previews, independent worker handoff and authority verification, retired-key migration protection, and TUI/JSON launch visibility. Verified with focused launcher, overlay, gate and failure tests plus all five repository gates; integrated 4050c17 and 23e9f0c into main and removed worktrees.
<!-- SECTION:FINAL_SUMMARY:END -->
