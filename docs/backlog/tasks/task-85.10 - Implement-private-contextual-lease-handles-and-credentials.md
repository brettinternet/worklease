---
id: TASK-85.10
title: Implement contextual handles and credential sources
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 05:56'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.7
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/lease_context.py
  - src/worklease/lease_file.py
  - src/worklease/credentials.py
  - tests/test_lease_context.py
  - tests/test_credentials.py
  - tests/test_cli.py
  - TASK-67
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 102000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement private client handles and exclusive credential selection per contract sections 4, 6.2 and 9. Own internal/handle and lifecycle CLI wiring. Independent loops must have stable session-scoped contexts or explicit handles; login agent identity is only metadata.

Persist generated acquire/transfer credentials and exact pending requests before authority dispatch. Use stable cross-process sibling locks and atomic fsync/rename updates, bind authorityId, and preserve recoverable pending state through crashes or failed final writes. Explicit credentials bypass contextual lookup and never mix with handle fields. Read-only inspection/verification does not create files or automatically dispatch pending mutations. No error path prints a token.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Context tests cover subdirectories, symlinks, linked worktrees and non-Git directories, two sessions in one checkout, and identical contention resources despite distinct sessions/handles.
- [ ] #2 Security tests reject unsafe/symlinked/hard-linked/foreign/oversized handles and credentials, validate the pending/ready schema, enforce strict token encoding and produce durable owner-only writes without secret diagnostics.
- [ ] #3 Two real processes acquiring different resources into one destination cannot orphan a grant or overwrite an active/pending handle; concurrent mutations reload under a stable lock, and transfer locks two distinct destinations in canonical order.
- [ ] #4 Crash tests at pre-dispatch, post-commit/pre-handle-write, release cleanup and both transfer-handle boundaries recover the original request and bearer, never rewind revision, never mint new intent and never leak tokens. Handle persistence tests keep a separate recoveryRequest beside a pending external request and clear both only after confirmed resolution; 85.9 owns end-to-end reconciliation crash tests.
- [ ] #5 Selector tests prove explicit credentials bypass even malformed unrelated contextual files, mixed modes fail, authority mismatch cannot mutate, pending recovery works, with explicit reconciliation integration owned by 85.9, stateless acquire retains required inputs, and mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
