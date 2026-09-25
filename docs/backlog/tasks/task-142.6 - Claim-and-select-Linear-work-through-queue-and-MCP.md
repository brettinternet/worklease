---
id: TASK-142.6
title: Claim and select Linear work through queue and MCP
status: Done
assignee: []
created_date: '2026-09-25 16:30'
updated_date: '2026-09-25 20:44'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.3
  - TASK-142.4
  - TASK-142.5
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: feature
ordinal: 70000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Once the Linear read/identity and sync path proves eligibility, expose existing Worklease claim lifecycle on exactly the same resources used by the CLI. Provider assignment or status is advisory, not a lease.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Claim for me, D11 authority consistency, the identity gate, and remote admission under coordination: work for Linear sources
- [x] #2 queue next --claim and MCP queue_next revalidate complete readiness and acquire only the stable Linear organization/issue resource; uncertain outcomes fail closed
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Trace the existing claim/next/MCP paths and Linear identity/readiness integration.
2. Connect Linear to the claim lifecycle and add focused regression coverage for identity, readiness and uncertain outcomes.
3. Run focused race tests and repository quality gates; review, commit, merge, and finalize on main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Worktree task-142-6-linear-claims: enabled stable Linear organization/issue claim domain confirmation without impossible complete list, accepted only finished known-item scans with complete fresh per-item closures for next/MCP, fixed paginated prerequisite reread, preserved guarded source identity, and labeled partial selection. CLI/MCP/TUI integration and uncertain acquire tests pass; focused race -count=3, full test, lint, format-check, typecheck and staged hooks passed. Awaiting one scoped safety review before commit/merge.

One adversarial review found non-Linear partial relation pages could be accepted; restricted paginated partial allowance to Linear, added regression TestQueueClaimRejectsNonLinearPartialDependencyPage, and reran focused race, lint, format, typecheck, and staged hooks successfully. Commit hook later hit unrelated existing TestQueueNextStartOutcomes MCP unknown-readback failure under concurrent full-suite load (claim TTL elapsed during a 42s subcase); awaiting competing test runs before retry without skipping hooks.

Delivered fbc49d3; merged on main as 7f5fcd1. On main, race -count=3 passed Linear CLI/MCP/TUI/uncertain-acquire/partial-page regressions and remote authority/admission plus portable Linear identity vectors. Main lint, format-check, typecheck, full test passed. Staged hooks and commit hook passed after competing suite load cleared; no hooks bypassed. Review finding fixed; no remaining blocker. Next resumable item is TASK-142.7, not started here.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Enabled Linear claims on verified organization/issue keys for TUI, CLI next, and MCP next, with fresh dependency/identity/authority gates and fail-closed partial-scan and uncertain-acquire behavior. Verified by focused race tests and all main quality gates; code fbc49d3 merged as 7f5fcd1.
<!-- SECTION:FINAL_SUMMARY:END -->
