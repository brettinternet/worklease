---
id: TASK-131.2
title: Gate launches and show the launched worker's claim
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-131.1
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-131
priority: high
type: feature
ordinal: 35000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Launch must be unavailable when the D11 authority check fails, and while the queue itself holds a claim on any of the item's resources, because the worker's own acquisition would then fail as already claimed (D18). The user can release or cancel (TASK-130.2) and then launch. That is two explicit steps, and another worker may acquire in between; the UI must say so. After launch, the queue shows the worker's claim once it appears in the overlay. It neither supervises nor retries the worker.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `x` opens a launch picker listing the configured actions, with a preview showing the argv after substitution, the cwd, the environment variable names (not values), and the authority
- [ ] #2 Launch is disabled with reason `authority-mismatch` when the D11 check fails, and with reason `queue-holds-claim` while the queue holds any of the item's resources. The latter explanation includes the release-then-launch race
- [ ] #3 queue query JSON reports launch availability and reasons for each action, matching the TUI
- [ ] #4 After launch, the worker's claim appears in the overlay attributed to its own session, and the queue never renews, releases, or supervises it
- [ ] #5 A reference launcher script in the repository consumes WORKLEASE_QUEUE_RESOURCES and WORKLEASE_QUEUE_AUTHORITY_ID, verifies the authority ID before acquiring, and acquires exactly the passed resources. An end-to-end test runs it for a GitHub item and for a Backlog.md item with a portable generic binding and asserts the same authority ID and resources as the queue
- [ ] #6 A successful process start is reported as launched, never as coordinated work. The item shows a worker claim only once one appears in the overlay, as tested with a launcher that exits without claiming
- [ ] #7 Tests cover each gate, the preview, and failure to start the process (reported with no claim side effects)
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
