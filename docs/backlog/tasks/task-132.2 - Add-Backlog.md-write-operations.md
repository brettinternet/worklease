---
id: TASK-132.2
title: Add Backlog.md write operations
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - backlog-md
milestone: m-1
dependencies:
  - TASK-132.1
references:
  - skills/worklease-workflow/references/source-providers/backlog-md.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-132
priority: high
type: feature
ordinal: 38000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The plan section 8 operation table enables these Backlog.md writes through documented `backlog task edit` flags only. State changes use `--status` with a configured status, reached through the TASK-132.1 intent mapping. Progress uses `--append-notes` or `--comment` with a trailing `worklease-op:` marker line. Assign to me is a read-modify-write with `--assignee`, which replaces the whole list; that is a declared race, and an assignee added by another writer between the read and the write can be lost.

Checking an acceptance criterion stays read-only: `--check-ac N` targets a mutable index, and pre/post reads can detect but not prevent checking the wrong criterion after a reorder.

Plan section 5 and TASK-126.6 cover Git effects. With auto_commit on, a write creates a commit and may run hooks, and the preview must show that. If TASK-126.6 found that auto-commit captures unrelated staged changes, writes in that mode stay disabled unless the TASK-126.6 findings recorded a safe condition.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Intent-mapped state changes accept only statuses configured in the project and are verified by read-back through `task view --json`
- [ ] #2 Record progress appends a note or comment ending in the operation's marker line and is verified with the TASK-132.1 rules
- [ ] #3 Assign to me writes the pre-read assignees plus me, and the preview discloses the race. If read-back shows an assignee from the pre-read is missing, or another writer's change is visible, the result is reported as a conflict, never as success, as tested with concurrent assignment edits
- [ ] #4 Check criterion is unavailable with reason `unstable-criterion-target`, and no `--check-ac` call is ever sent
- [ ] #5 When auto_commit is enabled, the preview states that a commit will be created and whether hooks run. Writes are disabled when the TASK-126.6 findings make auto-commit unsafe with staged changes
- [ ] #6 All writes go through the TASK-132.1 pipeline and use argv without a shell. Nothing edits task Markdown files directly
- [ ] #7 Integration tests in a scratch project cover each operation, marker verification, assignment conflict, and auto_commit on and off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
