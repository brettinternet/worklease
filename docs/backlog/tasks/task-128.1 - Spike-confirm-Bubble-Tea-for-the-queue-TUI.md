---
id: TASK-128.1
title: 'Spike: confirm Bubble Tea for the queue TUI'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 16:00'
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
- [x] #1 A prototype renders a 10,000-row list/detail layout while a background goroutine publishes a new snapshot every 100 ms. p95 and p99 input-to-render latency on the D22 reference machine are recorded
- [x] #2 Terminal resize, CJK and emoji width alignment, NO_COLOR, and stripping of CSI/OSC sequences embedded in row text are verified. Terminal.app, iTerm2, Ghostty, tmux, and a Herdr pane are each tested or recorded as unavailable
- [x] #3 The binary size delta, the added modules, and a clean `govulncheck` result for the added dependencies are recorded
- [x] #4 D9 is marked confirmed with pinned major versions, or replaced by an alternative with its rationale. The first open question in plan section 17 is resolved
- [x] #5 No prototype code is merged to main
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run doc-test` passes and every changed relative link resolves
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Prototype Bubble Tea/Lip Gloss in disposable scratch module with 10k rows and 100ms snapshot publisher; measure input-to-render latency on host. 2. Test width, resize, no-color and control-sequence safety and probe available terminal environments. 3. Record binary/dependency/vulnerability evidence, decide D9 and close the plan open question. 4. Run doc checks, commit only proposal and provider record, merge and clean worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Spike evidence and D9 decision committed on isolated branch e8d2eab. All project checks (doc-test, lint, format-check, test, typecheck, hooks) passed. Merge to main is waiting for the concurrent TASK-126.5 edit to docs/work-queue-tui-proposal.md; do not overwrite or stash its uncommitted work. Next: after that edit is committed, rebase/integrate proposal, verify, finalize and release.

2026-09-23 05:36 UTC: Rechecked integration after TASK-126.5 merged (main c8da592). Spike branch e8d2eab remains clean and ready, but primary checkout still has an uncommitted overlapping docs/work-queue-tui-proposal.md edit from concurrent tasks; do not merge or alter that work. Next: wait for primary proposal edit to be committed, integrate the spike commit, run doc-test and link checks, finalize and remove the verified owned worktree.

Integrated as main 3d3c463 (rebased e8d2eab) by applying the non-overlapping proposal hunk without touching concurrent uncommitted edits. doc-test, lint, format-check passed on the rebased branch; the diff adds no relative links. Only docs/work-queue-tui-proposal.md changed; no prototype code on main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Confirmed D9: Bubble Tea v1 (1.3.10) and Lip Gloss v1 (1.1.0). Recorded in-process p95 0.398 ms / p99 0.725 ms on 10k rows with 100 ms snapshots, width/resize/NO_COLOR/control-sequence stripping checks, tmux verified and other terminals recorded unavailable, +1.24 MB binary, added modules, clean govulncheck. Closed the section 17 pin question. Verified with mise run doc-test, lint, format-check.
<!-- SECTION:FINAL_SUMMARY:END -->
