---
id: TASK-128.8
title: Build the read-only list/detail TUI
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - tui
milestone: m-1
dependencies:
  - TASK-128.1
  - TASK-128.2
  - TASK-128.3
  - TASK-128.7
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: feature
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`worklease queue` is the human surface: a dense list/detail layout, Vim-first keys, and honest labels for every reason work is unavailable. Plan section 13 includes mockups. The TUI renders core snapshots only. Actions that arrive later (claim, assign, progress, launch) appear but are disabled with an explanation, so users can discover them. The Recovery tab arrives with TASK-132.4.

Provider text is untrusted: titles, bodies, and comments can contain terminal escape sequences and attacker-written content (plan sections 10 and 13). Use the library versions pinned by TASK-128.1.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease queue [--view NAME]` shows a header (view, authority profile and ID with local or remote scope, me, sources healthy/total, sync age), a views pane with counts, a sources pane with per-source status, the item list, and a detail pane with Summary, Dependencies, Activity, and Claims tabs
- [ ] #2 The keys in the plan section 13 table work for navigation, filtering, open, back, refresh, the command palette, help, and opening the provider URL. Keys for future actions show why the action is unavailable
- [ ] #3 Assignment, native claim state, and Worklease claim are separate columns. A claim detail always shows authority and scope. Blocked, occupied, assigned elsewhere, unknown dependencies, stale, permission denied, offline, and rate-limited states are each labeled distinctly and never shown as "no work"
- [ ] #4 The coverage line shows loaded versus total (exact, estimated, or unknown) and edge coverage. Search states whether it covers loaded rows or the whole source
- [ ] #5 The Dependencies tab shows each relationship's type, required outcome, observed evidence, coverage and freshness, and the exact reason the item is ready, blocked, or unknown
- [ ] #6 Session labels may be shortened for display, but JSON and stored identities always use full session IDs
- [ ] #7 The selection stays anchored by canonical identity across refreshes, and late detail responses never steal focus or reorder the list
- [ ] #8 Below 100 columns the TUI switches between list and detail instead of squeezing columns. NO_COLOR and high-contrast modes, resize, and Unicode width work, and state is conveyed with text labels, not color alone
- [ ] #9 Provider text is stripped of control and OSC sequences and rendered as bounded, sanitized text. Opening a URL requires an explicit key and uses safe argument passing. The TUI never opens raw Backlog.md files
- [ ] #10 Model and update tests drive the TUI with scripted key messages and snapshot inputs, with no real terminal. A render test proves that injected escape sequences are stripped
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
