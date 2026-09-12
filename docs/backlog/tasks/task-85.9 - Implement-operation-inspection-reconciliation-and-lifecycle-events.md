---
id: TASK-85.9
title: 'Implement operation inspection, reconciliation, events, and history'
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 05:58'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.8
  - TASK-85.10
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/reconciliation.py
  - src/worklease/operations.py
  - src/worklease/projections.py
  - tests/test_store.py
  - tests/test_history.py
  - TASK-69
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 101000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement operation inspection/reconciliation, events and history from contract sections 6.3, 7.4/7.5 and 11. Own internal/ledger, op/events/history CLI and minimal shared lease/handle wiring. Depend on multi-resource claims and handles so the actual recovery path can be exercised end to end.

Public inspection exposes metadata only; private full inspection authenticates the target epoch. Reconciliation identifies its own request separately from the target claim/operation, supports current-owner recovery of ended predecessors covering the full original resource set, and requires caller evidence of outcome and executor cessation. It never adopts an expired owner. Use authority/feed/filter-bound cursors with honest retention gaps.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Inspect tests cover public redaction, authenticated full receipt/evidence access, started-state reporting, historical credentials and ambiguous operation IDs resolved by explicit target claim ID.
- [ ] #2 Reconciliation tests cover expired predecessor recovery using a current covering claim, full-resource coverage checks, distinct target/resolver IDs, request-hash and evidence validation, idempotency/conflicts and transactional rollback.
- [ ] #3 Cursor tests cover bound authority/feed/resource identity, malformed input before storage open, ascending pages without skips under concurrent append, empty-feed watermark preservation and explicit GC gaps.
- [ ] #4 History correctly includes singleton and multi-resource epochs with public summaries, status and coverage; --full never exposes checkpoint/output/evidence without the separate authenticated inspection path.
- [ ] #5 CLI recovery from a pending handle is executable, including safe predecessor reconciliation without replaying external effects; text/JSON and mise run ci-go pass. Reconciliation from a pending exec retains both exact requests and survives pre-dispatch/post-commit crashes before atomically clearing the two slots. A definitive wrong-hash reconciliation clears only recoveryRequest so corrected evidence can be submitted while the original exec request remains; uncertain reconciliation retains both, confirmed resolution clears both.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
