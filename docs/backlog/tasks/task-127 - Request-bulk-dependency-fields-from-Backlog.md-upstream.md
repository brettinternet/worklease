---
id: TASK-127
title: Request bulk dependency fields from Backlog.md upstream
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - backlog-md
milestone: m-1
dependencies:
  - TASK-126.6
references:
  - 'https://github.com/MrLesk/Backlog.md'
documentation:
  - docs/work-queue-tui-proposal.md
priority: medium
type: task
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`backlog task list --json` and `search --json` omit dependencies, so a complete dependency graph costs one `task view --json` process per task (plan section 3, D13). If list JSON included `dependencies`, and ideally a per-task content version, the full graph would take one process, and most of the TASK-129.4 edge cache would become a fallback for older versions. The plan runs this request in parallel with the slices, so it gates nothing.

This is external communication on the user's behalf. Draft it with the writer agent under the user-voice skill, and post only after the user explicitly approves the final text. Check the latest Backlog.md release first. If it already provides the field, record the version instead of filing.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The latest Backlog.md release notes and `task list --json` output were checked, and the version is recorded
- [ ] #2 If the field is missing, the writer agent produced a draft upstream issue that cites the TASK-126.6 measurements and requests `dependencies` (and a per-task content version) in `task list --json` and `search --json`
- [ ] #3 The issue is posted only after explicit user approval, and its URL is recorded in this task and in plan section 14
- [ ] #4 If the field already exists, plan sections 3 and 14 and D13 are updated with the supporting version instead
<!-- AC:END -->
