---
id: TASK-132.6
title: Add `--start` to `queue next --claim` and MCP `queue_next`
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 16:44'
updated_date: '2026-09-24 23:07'
labels:
  - work-queue
  - mcp
milestone: m-1
dependencies:
  - TASK-132.5
  - TASK-130.6
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-132
priority: high
type: feature
ordinal: 49000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
After an agent claims through `queue next --claim` (D28), humans and non-Worklease readers still see the item as To Do until the agent eventually marks it In Progress. The claim already prevents agent contention, but the board should show the start right away. `--start` runs the TASK-132.5 Start work transition right after the acquire, through the same journaled write path, and reports it as a separate outcome. It is a convenience composition, not a lock and not cross-system atomicity.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `worklease queue next --view NAME --claim --start --json` and MCP `queue_next` with `claim: true, start: true` perform the TASK-132.5 steps (refresh, verify ownership, mapped transition through the TASK-132.1 pipeline, read-back) immediately after a successful acquire
- [x] #2 `--start` without `--claim` is rejected. Selection never skips a candidate because of its provider status write; only contention or revalidation moves to the next candidate
- [x] #3 The result reports the claim and transition outcomes separately (applied, rejected, not attempted, unknown). A rejected transition leaves the claim held and reports "Claim acquired; status unchanged"
- [x] #4 An uncertain transition enters recovery and is never retried or rolled back by the command. The claim stays with the caller
- [x] #5 When the source has no start mapping, the claim still succeeds and the transition is reported as not attempted with the capability reason. No status is invented
- [x] #6 Tests cover Backlog.md success, missing mapping, rejected transition, and uncertain transition through both CLI and MCP; GitHub Issues unsupported Start mapping reports not attempted without inventing a status
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D28) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Compose queue-next acquisition with a separate journaled Start work transition using the caller CLI/MCP handle. 2. Report separate claim and transition outcomes; preserve MCP hold and recheck live actor. 3. Verify Backlog.md outcomes via CLI/MCP and GitHub unsupported mapping at the capability layer; run gates and one review, commit and merge.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented CLI/MCP --start as a separate journaled transition after queue-next claim; CLI worker retains ordinary contextual handle, MCP retains lease handle. Focused CLI/MCP success, missing mapping, rejected, and unknown read-back scenarios pass; race count=3 passed. Lint, format-check, typecheck passed. Initial full test exposed an existing Start work fixture 30s claim expiry under suite load; aligned the fixture to the production default TTL and will rerun the full gate.

Single review identified MCP hold ceiling loss through CLI checkpoint and stale configured actor. Preserved MCP lease hold through checkpoint/read-back/recovery without changing ordinary CLI/TUI ownership behavior, and rechecked live Backlog actor before both prepare and confirmation; focused tests passed. Full suite rerun exposed an ordinary TUI checkpoint replay hash regression from an overly broad hold change, corrected by scoping preservation to MCP lease handles; rerunning gates.

User clarified AC #6: GitHub Issues has no Start mapping, so unsupported mapping is its required coverage; Backlog.md exercises all provider-write outcomes through CLI and MCP. No GitHub status mapping is added.

Integrated fe11eb4 into main by fast-forward. Post-rebase lint, format-check, full test, typecheck passed; staged hooks and targeted race count=3 passed. Reviewer found two concrete defects (MCP hold and actor drift), both fixed and directly retested. Worktree and associated idle Herdr workspace cleaned after same-commit verification. D28 and proposal §8 already specify this composition and unsupported mappings; no proposal decision or plan section was contradicted or refined.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added queue next --claim --start and MCP start with separate claim/transition outcomes. Backlog.md transitions use the existing journal and preserve CLI/MCP ownership; unsupported GitHub mapping remains claim-only. Verified CLI/MCP applied, rejected, missing-map and uncertain recovery paths, race count=3, all project gates and hooks; fe11eb4 merged into main. Review issues (MCP hold and actor drift) fixed.
<!-- SECTION:FINAL_SUMMARY:END -->
