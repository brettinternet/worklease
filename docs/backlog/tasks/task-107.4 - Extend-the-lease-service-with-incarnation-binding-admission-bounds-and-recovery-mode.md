---
id: TASK-107.4
title: >-
  Extend the lease service with incarnation binding, admission bounds, and
  recovery mode
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 03:23'
labels:
  - remote-authority
dependencies:
  - TASK-107.2
references:
  - internal/lease/service.go
  - internal/lease/helpers.go
  - internal/lease/reconciliation.go
  - internal/resource/resource.go
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 136000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Extend the typed lease service with remote domain invariants that must hold inside one serialized mutation. Accept a trusted actor context supplied by the authenticated boundary, keep caller installation identity separate from `agentId`, bind remote calls to `authorityId` and immutable `expectedRestoreId`, wrap responses with current restore identity and authority time, bind cursors to the restore incarnation, enforce portable-prefix and TTL/hold admission, and implement namespace recovery behavior. Authenticated remote reads enforce authority and incarnation against one coherent snapshot. Local callers use their existing local policy and do not need a remote restore identifier or remote admission configuration.

Persist each claim's admitted maximum TTL and absolute hold deadline. Every extension path and same-host transfer enforces those original limits. Administrative claim revocation preserves unresolved operations. Typed administrative reopening validates retained reconciliation and the structured private attestation, including installation coverage, pending-set coverage or an independently verified equivalent, known outcomes, old-authority and provider cessation, the selected durable backup cutoff or its explicitly unknown value, the interval through cessation or its unknown bounds, and permitted completed-history gaps. Unknown bounds do not waive exhaustive independent coverage. TASK-107.6 supplies authentication primitives and wires their current result into the same transaction so authorization cannot race the mutation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Remote service calls use a trusted actor context and enforce installation, role, authority, incarnation, epoch credential where applicable, retained replay, and new-admission checks in the frozen order inside one serialized transaction; local calls continue without remote identity or policy fields.
- [ ] #2 After successful installation authentication and role authorization, every installation-authenticated remote mutation fails with the frozen incarnation reason before replay or write when `expectedRestoreId` differs. Authenticated reads validate authority and incarnation against one coherent snapshot. Every response carries fresh `restoreId` and `authorityTime`, every cursor carries `restoreId`, and an old-incarnation cursor fails `authority-restored`.
- [ ] #3 Admission accepts only configured delimiter-terminated portable prefixes, always rejects reserved host-local prefixes, rejects rather than clamps excessive TTL or hold, and persists admitted limits that constrain heartbeat, begin, renew, and same-host transfer; transfer inherits the predecessor limits.
- [ ] #4 Changing configured prefixes or bounds affects new admission while retained exact replay and existing lifecycle, recovery, and same-host transfer continue under persisted limits.
- [ ] #5 Recovery mode refuses new operation starts. A recovery acquire must cover the full transitive unresolved resource closure atomically and fails when that closure exceeds 32 resources rather than splitting it; retired-prefix recovery remains allowed.
- [ ] #6 Retained exact replay is resolved before recovery or new-admission rejection. Completed operations return their retained receipts, and retained started operations return `unknown-outcome` without authorizing execution.
- [ ] #7 Administrative claim revocation records reason `revoked`, preserves unresolved started operations, appends the required public event, and makes the resource available under ordinary contention rules without claiming executor cessation.
- [ ] #8 Typed administrative reopening atomically validates reconciled retained rows and a complete private attestation, records its bounded references and gaps, and clears recovery mode. A selected durable cutoff and lost-history bound may be recorded as unknown only when exhaustive independent inventory, pending-set or equivalent coverage, outcomes, and cessation coverage are still established. Missing coverage keeps recovery mode active.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add typed remote actor/admission/recovery/admin contracts and stable reasons while preserving nil-context local behavior.
2. Thread incarnation and installation provenance through serialized lease reads/mutations and persisted claim, epoch, operation, reconciliation, and event rows.
3. Enforce portable-prefix and admitted TTL/hold limits across acquire, lifecycle extension, operation renewal/reconciliation, and same-installation transfer, with replay resolved before mutable admission policy.
4. Implement namespace recovery acquire/start behavior, administrative claim revocation, recovery status, and atomic evidence-validated reopening.
5. Bind ledger/watch cursors to restore incarnation and add focused tests for ordering, replay, limits, recovery closure, revocation, reopening, and unchanged local behavior.
6. Run focused tests, full repository gates, independent review/verification, fix findings, then commit and finalize TASK-107.4.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented typed remote actor/policy and response identity boundaries; persisted installation/restore/admitted-limit provenance across claims, epochs, operations, reconciliation, and events; enforced replay-first prefix/bound/recovery behavior across lease lifecycle and exact remote operation renewal; added administrative claim revocation, recovery status, evidence-validated atomic reopening, and restore-bound cursors. Focused and repository checks pass: go test ./internal/lease ./internal/ledger ./internal/watch; mise run lint; mise run format-check; mise run test; mise run typecheck.
<!-- SECTION:NOTES:END -->
