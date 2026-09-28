---
id: TASK-152
title: >-
  Stop queue next from resuming unclaimed in-progress work without release
  evidence
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-28 21:47'
updated_date: '2026-09-28 23:30'
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
- [x] #1 queue next (CLI and MCP) never selects an in-progress item whose prior Worklease claim expired without a recorded release or handoff
- [x] #2 The resume policy for in-progress items with no Worklease claim history is decided and recorded as a decision in docs/work-queue-tui-proposal.md
- [x] #3 worklease instructions loop, skills/worklease-workflow/SKILL.md, and docs/queue.md describe the same resume policy as the implementation
- [x] #4 A regression test at the selection layer covers an expired-claim in-progress item
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Apply approved open-only selection policy; reject start/resume of in-progress work in action eligibility.
2. Add selection regression coverage and align loop instructions, workflow skill, queue docs, and proposal decision.
3. Run focused and required repository checks, review, finalize task, commit on main, and push.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
User approved open-only policy, recorded as D34. SelectWave excludes all in-progress items (including explicit selectors); EvaluateAction rejects fresh in-progress state before acquisition. Selection-layer regression covers expired and free/no-history observations, action gates, and fallback to open work. CLI and MCP share the queue selection path. Loop instructions, workflow skill/example, and queue docs aligned.
Validation: focused changed tests passed with -race -count=3; mise lint, format-check, typecheck, test passed. Both race task phases passed: initial mise run race completed non-Queue phase before tool timeout; remaining Queue phase passed separately with WORKLEASE_PROVIDER_TESTS=1 go test -race -p 1 -parallel 2 -run ^TestQueue ./.... Independent reviewer found no validated defects. Delivery pending commit/push on main.
<!-- SECTION:NOTES:END -->
