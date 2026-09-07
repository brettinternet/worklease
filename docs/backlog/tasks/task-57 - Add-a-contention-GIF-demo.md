---
id: TASK-57
title: Add a contention GIF demo
status: Done
assignee:
  - '@brett'
created_date: '2026-09-07 14:21'
updated_date: '2026-09-07 14:36'
labels: []
dependencies: []
modified_files:
  - README.md
  - docs/demo.tape
  - docs/demo.gif
type: docs
ordinal: 58000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Show a straightforward two-worker Worklease workflow where both local CLIs compete for one fake resource, the first worker completes and releases it, and the waiting worker then acquires it. Use a clean tmux presentation without a bottom status line or hostname prompts.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A reproducible VHS tape renders a two-pane tmux contention workflow
- [x] #2 The rendered GIF shows the first worker holding and releasing the fake resource before the waiting worker acquires it
- [x] #3 The demo has no tmux bottom status line and no hostname in either prompt
- [x] #4 Project documentation displays the rendered demo
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a reproducible VHS tape that creates a shared temporary Worklease state and presents two named tmux panes without status chrome.
2. Demonstrate one worker acquiring and running guarded work while a second waits for the same fake resource, then acquires after release.
3. Render the GIF, embed it near the README introduction, and run documentation and project quality checks.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Added docs/demo.tape with a shared temporary authority, named worker prompts, hidden tmux status, an immediate contention failure, bounded waiting, release, successor acquisition, and cleanup. Rendered docs/demo.gif and embedded it in README.md.

Validation: VHS 0.11.0 rendered the 1400×720 GIF successfully. Manual inspection of frames at 10s, 20s, and 30s confirmed worker-a acquisition, worker-b already-claimed and waiting states, worker-a release, worker-b acquisition/release, named hostname-free prompts, and no tmux status line. `mise run lint`, `mise run format-check`, `mise run test` (272 tests), and `mise run typecheck` all passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added and rendered a reproducible two-worker contention demo and embedded it in the README. Verified the complete conflict/wait/handoff sequence visually from rendered frames and passed all project quality gates.
<!-- SECTION:FINAL_SUMMARY:END -->
