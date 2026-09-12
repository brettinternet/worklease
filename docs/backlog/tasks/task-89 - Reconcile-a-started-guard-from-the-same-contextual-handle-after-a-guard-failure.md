---
id: TASK-89
title: >-
  Reconcile a started guard from the same contextual handle after a guard
  failure
status: Done
assignee:
  - '@brett'
created_date: '2026-09-12 22:39'
updated_date: '2026-09-12 23:11'
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
- [x] #1 op reconcile from a contextual or explicit handle whose pending guard request left the handle revision behind succeeds against the current authenticated revision without rewinding it
- [x] #2 After a confirmed reconciliation the handle is ready at the authority's current revision and heartbeat, checkpoint, and a new exec succeed
- [x] #3 An executable journey covers exec timeout, op inspect, same-handle op reconcile, and heartbeat, and runs in the CI documentation or E2E gate
- [x] #4 Reconciliation still rejects a wrong expected request hash, a changed evidence replay, and credentials that do not belong to the resolver claim
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add an atomic reconciliation mode that authenticates the resolver token but adopts the current authority revision only when a handle is recovering its own started operation.
2. Route same-claim pending handle reconciliation through that mode while preserving strict revision checks for ordinary and predecessor reconciliation.
3. Add service and CLI regression coverage for stale-handle success, post-reconcile lifecycle readiness, wrong hash/evidence replay/foreign credentials, plus a built-binary timeout→inspect→same-handle reconcile→heartbeat journey in the E2E smoke gate.
4. Run focused tests and all repository quality gates, review the diff, finalize TASK-89, commit, merge to main, and clean up the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented atomic current-revision reconciliation for a pending handle's own started operation while retaining strict revision checks for ordinary/predecessor reconciliation. Routed the CLI only when the pending request matches the target claim and operation. Added service and CLI regressions plus built-binary E2E recovery coverage.

Verification passed: go test ./internal/lease ./internal/cli; mise run e2e; mise run lint; mise run format-check; mise run test; mise run typecheck; mise run race; mise run vuln; mise run man; git diff --check.

Independent review found two replay edges: an active claim could advance after reconciliation committed but before handle recovery, and an ended claim could make a known committed replay look uncommitted. Fixed both by authenticating live claim state on current-revision replay, synchronizing only active handles to the live revision/expiry, and preserving committed recovery state when the claim is ended. Added active-renewal and ended-claim regressions.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Same-handle guard reconciliation now authenticates and atomically adopts the current authority revision without weakening ordinary revision fencing. Exact replays synchronize active handles to live revision/expiry and preserve committed recovery state rather than reviving ended claims. Added service/CLI security regressions and a built-binary timeout→inspect→reconcile→heartbeat/checkpoint/exec E2E journey. Verified with focused tests, E2E, lint, format, test, typecheck, race, vulnerability, manpage, and diff checks; independent review findings were fixed.
<!-- SECTION:FINAL_SUMMARY:END -->
