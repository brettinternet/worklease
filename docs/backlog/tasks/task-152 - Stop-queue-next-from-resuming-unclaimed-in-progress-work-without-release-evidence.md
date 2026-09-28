---
id: TASK-152
title: >-
  Stop queue next from resuming unclaimed in-progress work without release
  evidence
status: To Do
assignee: []
created_date: '2026-09-28 21:47'
labels:
  - work-queue
dependencies: []
priority: medium
type: bug
ordinal: 92000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
queue next --claim treats any unclaimed item the provider reports In Progress as resumable (internal/queue/selection.go SelectWave; EvaluateAction ActionResume in internal/queue/readiness.go; documented in docs/queue.md 'An unclaimed in-progress item can be resumed'). The loop instructions (worklease instructions loop, step 7) say claim expiry does not prove the prior worker stopped and does not authorize resuming in-progress work without explicit handoff or authoritative abandonment evidence. The two now conflict more often because agent guidance recommends queue next --claim --start: a worker that starts an item and then crashes leaves it In Progress with an expired claim, and the next agent's queue next can pick it up while the first worker may still be running. Items a human set to In Progress without a Worklease claim are also selected. Decide the resume policy (for example require release/handoff evidence from the authority's lifecycle history, or exclude in-progress items unless the caller owns them) and make selection, instructions, the workflow skill, and docs agree.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 queue next (CLI and MCP) never selects an in-progress item whose prior Worklease claim expired without a recorded release or handoff
- [ ] #2 The resume policy for in-progress items with no Worklease claim history is decided and recorded as a decision in docs/work-queue-tui-proposal.md
- [ ] #3 worklease instructions loop, skills/worklease-workflow/SKILL.md, and docs/queue.md describe the same resume policy as the implementation
- [ ] #4 A regression test at the selection layer covers an expired-claim in-progress item
<!-- AC:END -->
