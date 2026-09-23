---
id: TASK-135
title: Verify the queue TUI in native terminals
status: To Do
assignee: []
created_date: '2026-09-23 21:10'
labels:
  - work-queue
  - tui
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
- [ ] #1 `worklease queue` is exercised against a 10,000-row fixture in Terminal.app, iTerm2, Ghostty, tmux, and a Herdr pane, recording terminal versions
- [ ] #2 In each terminal: resize across the 100-column list/detail switch, CJK and emoji alignment, NO_COLOR, and a row containing CSI/OSC sequences render correctly and leave the terminal state intact after exit
- [ ] #3 Defects are fixed or filed as tasks, and plan section 3 records the results
<!-- AC:END -->
