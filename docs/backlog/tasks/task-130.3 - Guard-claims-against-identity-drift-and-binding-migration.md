---
id: TASK-130.3
title: Guard claims against identity drift and binding migration
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 04:55'
labels:
  - work-queue
  - authority
milestone: m-1
dependencies:
  - TASK-129
references:
  - internal/resource/resource.go
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-130
priority: high
type: feature
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Claims exclude each other only while every contender derives the same resource for the same item (plan section 6). Three events silently split that: a Backlog.md duplicate ID repair renumbers a task (and clones can allocate the same ID independently), a GitHub repository is renamed, or an issue is transferred. D12 requires the portable Backlog.md binding to reject duplicate IDs before enabling claims and again before each acquisition. D24 requires any detected rename, transfer, or ID repair to disable claims until the user explicitly rebinds.

D12 also makes adopting a portable binding an exclusion-domain migration. A worker still using the default `backlog-md` policy does not contend with the portable key, even on the same authority, so old workers must stop, old claims and operations must be resolved, and CLI, skill, and launch callers must switch together. The queue can check its own authority for old-key claims but cannot discover other claim domains, and it must say so.

Rebinding means editing the source in queue.yaml. No alias may create a second, simultaneously writable claim domain. This task delivers the gate as action availability plus a pre-acquisition check; TASK-130.1 calls it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 For a backlog-md source with a generic binding, claims are disabled with reason `duplicate-item-id` while list output contains repeated IDs. The pre-acquisition check re-reads the list and fails on duplicates
- [x] #2 When queue.yaml enables or changes a generic binding, claims for that source stay disabled with reason `binding-migration-required` until the user confirms a migration checklist (stop old workers, resolve old claims and operations, update CLI, skill, and launch callers). Confirmation is refused while the view's authority holds active claims on the source's items under the previous keys, and the checklist states that claims in other authorities cannot be detected
- [x] #3 A detected GitHub rename or transfer (from TASK-128.5) disables claims for the affected source or item with reason `identity-changed`, and shows the old and new locators plus the rebind steps
- [x] #4 A Backlog.md task whose ID disappears while an active claim exists on its key in the view's authority is reported as an identity migration and is never silently re-keyed
- [x] #5 The same migration gate applies to any other change of a source's claim inputs, such as a GitHub repository rebind
- [x] #6 The queue never derives a key from a resolved immutable ID in place of the configured locator (D24), as tested against the TASK-126.4 vectors
- [x] #7 Tests cover duplicate detection in availability and in the pre-acquisition check, migration refusal with old-key claims present, rename, transfer, renumber, and blocked rebind
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Map existing queue identity, diagnostics, claim overlay, and private configuration lifecycle.
2. Add fail-closed identity and binding migration gates with explicit confirmation and fresh pre-acquisition verification; retain configured key inputs.
3. Exercise duplicate, drift, active old-claim, rebind and renumber tests; run project quality gates, review, commit and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented owner-private identity receipts, confirmation with old-key checks, fresh duplicate/renumber gates, GitHub drift locator diagnostics, and exported pre-acquisition multi-key verification. Focused tests pass; verifier found stale-list and pre-acquisition gaps, now fixed and regression-tested. Running final gates and integration.

Verification: go test ./... including TestQueueIdentityConfirmationAndRebind (held old-key refusal, explicit confirmation, changed binding), TestQueueIdentityConfirmationRejectsDuplicateIDs, TestIdentityDuplicateGuardRechecksFreshList, TestPreAcquireIdentityUsesFreshListAndBothClaimDomains, TestIdentityRenameTransferRenumberAndRebind, and GitHub rename/transfer tests. mise run lint, format-check, test, typecheck, hooks all passed. One independent verifier pass found two concrete defects (stale list false presence; missing pre-acquisition recheck); fixed both and reran checks. Code commit 55b4de41bd1f92a171343a3c8dd86a06395512d9 fast-forward merged to main. TASK-130.1 must call PreAcquireIdentity immediately before its atomic acquisition.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added private claim-identity confirmation, duplicate/renumber and GitHub drift gates, and a fresh pre-acquisition multi-key check. All seven acceptance criteria verified by focused and full tests; five quality gates passed. Merged 55b4de4 to main. Next: TASK-130.1 wires PreAcquireIdentity into acquisition.
<!-- SECTION:FINAL_SUMMARY:END -->
