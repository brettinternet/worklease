---
id: TASK-132.6
title: Add `--start` to `queue next --claim` and MCP `queue_next`
status: To Do
assignee: []
created_date: '2026-09-23 16:44'
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
- [ ] #1 `worklease queue next --view NAME --claim --start --json` and MCP `queue_next` with `claim: true, start: true` perform the TASK-132.5 steps (refresh, verify ownership, mapped transition through the TASK-132.1 pipeline, read-back) immediately after a successful acquire
- [ ] #2 `--start` without `--claim` is rejected. Selection never skips a candidate because of its provider status write; only contention or revalidation moves to the next candidate
- [ ] #3 The result reports the claim and transition outcomes separately (applied, rejected, not attempted, unknown). A rejected transition leaves the claim held and reports "Claim acquired; status unchanged"
- [ ] #4 An uncertain transition enters recovery and is never retried or rolled back by the command. The claim stays with the caller
- [ ] #5 When the source has no start mapping, the claim still succeeds and the transition is reported as not attempted with the capability reason. No status is invented
- [ ] #6 Tests cover both adapters for success, missing mapping, rejected transition, and uncertain transition through both the CLI and MCP
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D28) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
