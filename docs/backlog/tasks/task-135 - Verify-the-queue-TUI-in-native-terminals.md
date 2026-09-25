---
id: TASK-135
title: Verify the queue TUI in native terminals
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 21:10'
updated_date: '2026-09-25 21:46'
labels:
  - work-queue
  - tui
  - reviewed
milestone: m-1
dependencies: []
documentation:
  - docs/work-queue-tui-proposal.md
priority: medium
type: task
ordinal: 50000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The TASK-128.1 spike verified Bubble Tea only in tmux; Terminal.app, iTerm2, Ghostty, and a Herdr pane were unavailable in the unattended run. Plan section 3 (TUI spike) and section 13 require testing the shipping TUI in all of them. TASK-128.8 covered this only with scripted model tests, not real terminals. This needs an interactive session on the D22 reference machine (Apple M1 Max, 32 GiB); end-to-end input-to-render latency against the section 14 budget stays in TASK-129.6.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `worklease queue` is exercised against a 10,000-row fixture in Terminal.app, iTerm2, Ghostty, tmux, and a Herdr pane, recording terminal versions
- [x] #2 In each terminal: resize across the 100-column list/detail switch, CJK and emoji alignment, NO_COLOR, and a row containing CSI/OSC sequences render correctly and leave the terminal state intact after exit
- [x] #3 Defects are fixed or filed as tasks, and plan section 3 records the results
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Build a disposable 10,000-row fixture and exercise the shipping queue in available native terminal environments.
2. Record versions and verify resize, wide characters, color-off, escape sanitization, and terminal restoration.
3. Fix or file concrete defects, update plan section 3, run gates, integrate and finalize only when all terminal evidence is complete.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Attempted on D22 Apple M1 Max / 32 GiB. Generated 10,000-row fixture at /private/tmp/worklease-queue-135-eKeeRR (Backlog.md 1.52.0); recorded versions: tmux 3.7c, Terminal.app 2.15, iTerm2 3.7.2, Ghostty 1.3.1. A detached tmux run displayed 10,000 cached rows at 120x35 and 80x25, with split detail above 100 columns; Herdr job also displayed 10,000 cached rows but exited 130. Initial queue startup failure was an incomplete private queue.yaml (missing required view filter), not a product defect. A valid escaped CJK/emoji/CSI/OSC first row was added to the source, but the old index still held the original title; a fresh-cache tmux run showed source offline/unknown and 0 rows, so no escape or NO_COLOR outcome is verified. The user initially reported GUI checks passed, then confirmed they cannot verify the updated fresh-cache checks; do not count the GUI checks as acceptance evidence. No source files changed, no project gates run, no commit/merge; the clean worktree was removed. Blocker: controlled interactive session for Terminal.app, iTerm2, Ghostty and Herdr with actual observed first-row and NO_COLOR results. Next: diagnose fresh-cache source offline, run script in all terminals and tmux, verify 100-column switch, CJK/emoji/CSI/OSC, NO_COLOR, and prompt restoration, then document plan section 3 and run gates.

Resolved fresh-cache source-resolve-failed: the fixture checkout is outside the repo, so its mise backlog shim had no selected version. The disposable launcher now prepends the installed Backlog.md 1.52.0 bin directory; the fresh index populated 10,000 rows. tmux 3.7c captures at 120x35 and 80x25 showed the sanitized 仕事 🙂 BAD row and detail/list resize; NO_COLOR=1 capture had no styling escapes, and q closed the session. User explicitly confirmed the 10,000-row, 100-column resize, CJK/emoji/CSI/OSC, NO_COLOR and post-q prompt checks in Terminal.app 2.15, iTerm2 3.7.2, Ghostty 1.3.1 and Herdr 0.9.1. No product defect observed; results documented in plan section 3. mise run lint, format-check, test, typecheck passed.

Verification documentation and task completion committed on main as 8cbe670 (no product source change).

Post-completion review: documentation-only verification task; plan section 3 record matches ticket evidence. No defects and no follow-up needed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Verified the shipping queue against 10,000 rows in Terminal.app, iTerm2, Ghostty, tmux and Herdr; sanitized rendering, resize, NO_COLOR and terminal restoration passed. Fixed the fixture launcher PATH (not product code), documented results in plan section 3, and passed project gates.
<!-- SECTION:FINAL_SUMMARY:END -->
