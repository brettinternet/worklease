---
id: TASK-7
title: Add sanitized verbose lease diagnostics
status: To Do
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:14'
updated_date: '2026-07-14 03:33'
labels:
  - coordination
  - recovery
  - cli
  - security
dependencies: []
references:
  - src/worklease/cli.py
  - src/worklease/store.py
  - src/worklease/sqlite.py
  - tests/test_cli.py
  - tests/test_store.py
  - tests/test_execution.py
  - README.md
  - skills/worklease-workflow/SKILL.md
priority: medium
type: enhancement
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add a read-only verbose status projection for one opaque resource so operators can diagnose active and expired claims and durable unknown operation intents after crashes without unsafe replay or bearer-token exposure.

Extend the existing status surface rather than adding a recovery mutation. Default JSON and text output must remain unchanged. The projection may read the existing epochs, claims, operations, and releases tables, but it must use an explicit non-secret field allowlist. Show current claim metadata and unresolved started operation summaries; when release metadata is available, show only the released claim ID, release operation ID, revision, and timestamp. Reclaimed is an acquire-response fact, not persisted historical state, so diagnostics must not present it as a stored status.

Raw operations.receipt and releases.receipt values must never be returned: they can contain bearer tokens. Do not expose raw requests, command output, provider payloads, file contents, or tokens. Keep provider checkpoints, dependency selection, messaging, orchestration, force-reclaim, claim adoption, and unknown-operation reconciliation writes out of scope; TASK-6 remains the separate reconciliation proposal.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease status --resource RESOURCE --verbose` emits a schema-versioned read-only diagnostic projection for free, active, and expired resources without changing the existing default status response.
- [ ] #2 The projection reports only the allowlisted non-secret claim, unresolved-operation, and release metadata; it never reports bearer tokens, raw receipts, requests, command output, provider payloads, or file contents.
- [ ] #3 Started operations are clearly identified as unknown outcomes with operation ID, kind, expected revision, and creation time, plus safe non-mutating guidance; completed operation receipts are not dumped.
- [ ] #4 Diagnostics preserve opaque resource identity, active/expired semantics, stable ordering, existing exit codes, and the unchanged local-coordination/provider-fencing guarantee; they do not reclaim, adopt, reconcile, heartbeat, release, or otherwise mutate state.
- [ ] #5 Automated tests cover free/active/expired resources, unresolved started operations, release metadata, deterministic JSON and text projections, sentinel token/receipt redaction, malformed input, and the absence of side effects.
- [ ] #6 README and workflow documentation define the verbose fields, redaction rules, unknown-outcome interpretation, and safe reacquisition flow; they explicitly state that `reclaimed` is reported by acquire only and is not historical diagnostic state.
- [ ] #7 The implementation reuses the existing SQLite schema without a provider checkpoint API or new provider/network dependency, and focused lint, typecheck, format, and test checks pass.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Redacted diagnostic projection (AC1-AC4, AC7).** Add a read-only LeaseStore projection over existing epochs, claims, operations, and releases. Return only the settled allowlist, classify free/active/expired state at one captured clock value, include unresolved started operations in stable creation-time/ID order, and perform no writes or schema migration.
2. **[T2] Deterministic JSON/text CLI and tests (AC1-AC5).** Add `status --verbose` while preserving byte-for-byte default status behavior and exit codes. Implement deterministic human-readable text, sentinel-secret tests, malformed-input tests, and before/after database assertions proving no side effects.
3. **[T3] Operator guidance (AC6, AC7).** Document every verbose field, unknown-outcome guidance, acquire-only `reclaimed` semantics, safe wait/reacquire behavior, and the unchanged local-coordination/provider-fencing boundary in README and workflow guidance.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now.

**Goal and target area:** Extend `LeaseStore.status` through a separate verbose projection and `worklease status --verbose`; reuse current SQLite tables and existing README/workflow/test files. All references exist.

**Resolved decisions:** The allowlist is: resource; state; current claim claimId/agentId/sessionId/ownerId/workKey/coordinationOnly/revision/acquiredAt/heartbeatAt/expiresAt; unresolved operation operationId/kind/expectedRevision/createdAt; latest release claimId/operationId/revision/releasedAt. Unknown operations sort by createdAt then operationId. No token, raw request/receipt, output, provider payload, file content, checkpoint body, or completed receipt is returned. State is evaluated once per request. Text output uses the same projection; default status output remains unchanged. Guidance is static and non-mutating: wait for active ownership or acquire after expiry, then use TASK-6 reconciliation when available.

**Non-goals:** schema changes, recovery writes, checkpoint display, dependency/provider operations, reclaim/adoption, reconciliation, network dependencies.

**Evidence and assumptions:** Current CLI status accepts only resource; current epochs/claims/operations/releases tables contain the required safe columns. `reclaimed` is not persisted and therefore remains acquire-only.

**Task/acceptance map:** T1→AC1-4/7; T2→AC1-5; T3→AC6/7.

**Pending verification:** deterministic free/active/expired projections, unknown sorting, release fields, byte-stable default status, sentinel-secret search, database no-change assertion, quality gates.

**Next action:** implement T1 as a read-only store method.

**Refinement checkpoint:** refined: TASK-7 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=934168e6-119d-48dd-a030-8729e23634c7; claimRevision=11; refinement: complete.
<!-- SECTION:NOTES:END -->
