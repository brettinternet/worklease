---
id: TASK-89
title: >-
  Reconcile a started guard from the same contextual handle after a guard
  failure
status: To Do
assignee:
  - '@brett'
created_date: '2026-09-12 22:39'
labels:
  - go-rewrite
dependencies: []
references:
  - internal/cli/ledger_commands.go
  - internal/lease/reconciliation.go
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
priority: high
type: bug
ordinal: 114000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Reproduced after TASK-88: run `worklease exec --max-duration 100ms -- sleep 5` under a contextual handle, then `worklease op reconcile` from that same handle with the inspected request hash. It fails `stale-revision` because the handle still holds revision 1 while the authority advanced the claim when it committed the started intent and each internal renewal. `heartbeat` is blocked by the pending exec, so the only recovery is to wait for expiry and reacquire into a new handle. Contract section 9 says `op reconcile` may select credentials from a pending handle and that handle recovery reads the current authenticated revision and never rewinds it; section 7.5 expects the live owner to resolve its own started operation. The existing reconcile test only covers the successor-claim path (internal/cli/ledger_commands_test.go), which is why this was never caught. Reproduction state: /private/tmp/worklease-review/home-reconcile.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 op reconcile from a contextual or explicit handle whose pending guard request left the handle revision behind succeeds against the current authenticated revision without rewinding it
- [ ] #2 After a confirmed reconciliation the handle is ready at the authority's current revision and heartbeat, checkpoint, and a new exec succeed
- [ ] #3 An executable journey covers exec timeout, op inspect, same-handle op reconcile, and heartbeat, and runs in the CI documentation or E2E gate
- [ ] #4 Reconciliation still rejects a wrong expected request hash, a changed evidence replay, and credentials that do not belong to the resolver claim
<!-- AC:END -->
