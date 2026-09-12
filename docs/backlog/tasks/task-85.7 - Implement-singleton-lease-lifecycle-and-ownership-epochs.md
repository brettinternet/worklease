---
id: TASK-85.7
title: Implement the claim lifecycle service
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 05:56'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.5
  - TASK-85.6
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/claims.py
  - src/worklease/acquisition.py
  - src/worklease/lifecycle.py
  - src/worklease/operations.py
  - src/worklease/models.py
  - tests/test_store.py
  - tests/test_cli.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 99000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement the claim lifecycle service and initial lifecycle CLI against the amended contract sections 4, 6–8, 18 and 20. Own internal/lease and acquire/status/list/heartbeat/checkpoint/release/transfer commands. This slice supports exactly one resource using the shared resource-list model; 85.8 lifts the limit.

The authority accepts client-held credentials and never returns tokens. Until 85.10 adds pending handles, expose the stateless path with caller-retained identity/credential/request inputs. Implement acquire replay, epoch-authenticated lifecycle replay, exact normalized request hashes including maxDuration and replay deadline, contention/wait, conservative clock handling, checkpoint recovery, atomic transfer/release, and started-operation primitives. One mutation has one operation identity, while a guard's start/renew/complete transitions share its operation row.

The service carries authority identity and typed domain results; it does not accept CLI paths, remote transports, or provider writes. Keep all lifecycle events transactional and make unresolved predecessor operations visible to later guard/reconciliation consumers.

Provide authenticated operation lookup and current-state synchronization for 85.10 pending-handle recovery; 85.9 adds user-facing ledger projections and reconciliation on top.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Lifecycle tests prove atomic acquire/renew/checkpoint/release/transfer and epoch end/checkpoint recovery, client-held 64-hex credentials with hashes only in authority state, successor revision 1, release audit reason and no transfer free interval.
- [ ] #2 Authorization tests prove ordered current-claim/token/expiry/revision checks without failed-write side effects, exclusive explicit selection, bounds validation and token-free public status/list.
- [ ] #3 Replay tests authenticate original epochs after release/transfer, return recorded receipts without reviving ownership, reject changed TTL/maxDuration/cwd/content intent, enforce requestNotAfter and preserve current revisions; no replay returns a token.
- [ ] #4 Clock/wait tests cover forward expiry, bounded small rollback clamping, larger regression failing closed without re-anchoring, monotonic jittered wait deadlines and redacted contention metadata.
- [ ] #5 Cross-process contention has exactly one winner; started-operation tests reject overlapping guarded starts and unrelated lifecycle mutations, permit only guard-internal renewal/completion, and expose predecessor unknowns after expiry; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
