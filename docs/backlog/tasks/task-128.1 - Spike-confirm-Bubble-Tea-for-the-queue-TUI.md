---
id: TASK-128.1
title: 'Spike: confirm Bubble Tea for the queue TUI'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - tui
milestone: m-1
dependencies: []
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: spike
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
D9 selects Bubble Tea and Lip Gloss, pending confirmation. The TUI must hold p95 input-to-render within 50 ms on 10,000 cached rows during a background refresh (plan section 14). It must also handle resize and Unicode width, honor no-color, and never let provider text alter the terminal (plan section 13). D8 ships the TUI in the main binary, so dependency weight and vulnerability exposure matter too.

The spike has no prerequisites and may start before S1 finishes. Build a throwaway prototype on a branch or in a scratch module. Commit only the recorded decision and measurements, not the prototype.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A prototype renders a 10,000-row list/detail layout while a background goroutine publishes a new snapshot every 100 ms. p95 and p99 input-to-render latency on the D22 reference machine are recorded
- [ ] #2 Terminal resize, CJK and emoji width alignment, NO_COLOR, and stripping of CSI/OSC sequences embedded in row text are verified. Terminal.app, iTerm2, Ghostty, tmux, and a Herdr pane are each tested or recorded as unavailable
- [ ] #3 The binary size delta, the added modules, and a clean `govulncheck` result for the added dependencies are recorded
- [ ] #4 D9 is marked confirmed with pinned major versions, or replaced by an alternative with its rationale. The first open question in plan section 17 is resolved
- [ ] #5 No prototype code is merged to main
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run doc-test` passes and every changed relative link resolves
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
