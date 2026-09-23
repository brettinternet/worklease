---
id: TASK-126
title: 'Work queue S1: contracts, identity vectors, and provider probes'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 16:18'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-126.1
  - TASK-126.2
  - TASK-126.3
  - TASK-126.4
  - TASK-126.5
  - TASK-126.6
  - TASK-126.7
documentation:
  - docs/work-queue-tui-proposal.md
priority: high
type: task
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Every later work-queue slice builds on three foundations that are not settled yet. The first is contract semantics for capabilities, coverage, and freshness. The second is claim resources that are byte-identical between queue and CLI callers. The third is provider behavior the plan could not verify offline: GitHub sync behavior, Backlog.md cost at 10,000 tasks, and Git side effects. A mistake in any of them would be built into every adapter. See plan sections 2, 3, 6, 7, and 16 (S1).

The upstream Backlog.md bulk-dependency request (TASK-127) runs in parallel and does not gate S1.

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every S1 child task is Done, with its evidence recorded in the child task
- [x] #2 Dependency fixtures distinguish hard edges from hierarchy, cross-source references, cycles, partial graphs with known blockers, and unsupported completion conditions
- [x] #3 Plan section 3 contains the S1 probe results. Any decision they change is updated in section 2 and in the dependent plan sections
- [x] #4 contract.md, source-provider-contract.md, and the Backlog doc `doc-1 - Worklease-Workflow` agree with the plan and with each other
- [x] #5 The TASK-126.4 identity vectors run in `mise run test`
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Re-verify all S1 child evidence and main-branch contracts, fixture coverage, proposal probe results, and full test suite; record criteria evidence and finalize the integration task.

Correct the stale pre-S1 cancellation wording in proposal section 8 before final verification.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Main at 7fbb964: all seven children Done with individual evidence. Dependency eligibility v1 JSON cases cover hard/hierarchy, cross-source, cycle, partial known blocker, and unsupported condition (jq assertions passed). Proposal §3 contains Backlog 10k and GitHub probes with D13/D14 and §14 updates. Cross-checked contract.md, source-provider-contract.md, and doc-1; corrected stale pre-S1 cancellation text in proposal §8. mise run lint, format-check, test, typecheck, doc-test and focused TestVersionedKeyVectors all passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Re-verified S1 on main; corrected obsolete cancellation wording. All five criteria satisfied by child receipts, dependency fixture assertions, aligned contracts/proposal, and full checks including identity vectors.
<!-- SECTION:FINAL_SUMMARY:END -->
