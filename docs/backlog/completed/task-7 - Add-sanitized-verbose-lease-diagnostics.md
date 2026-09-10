---
id: TASK-7
title: Add sanitized verbose lease diagnostics
status: Done
assignee:
  - '@codex-loop-main'
created_date: '2026-07-14 02:14'
updated_date: '2026-07-14 07:10'
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
- [x] #1 `worklease status --resource RESOURCE --verbose` emits a schema-versioned read-only diagnostic projection for free, active, and expired resources without changing the existing default status response.
- [x] #2 The projection reports only the allowlisted non-secret claim, unresolved-operation, and release metadata; it never reports bearer tokens, raw receipts, requests, command output, provider payloads, or file contents.
- [x] #3 Started operations are clearly identified as unknown outcomes with operation ID, kind, expected revision, and creation time, plus safe non-mutating guidance; completed operation receipts are not dumped.
- [x] #4 Diagnostics preserve opaque resource identity, active/expired semantics, stable ordering, existing exit codes, and the unchanged local-coordination/provider-fencing guarantee; they do not reclaim, adopt, reconcile, heartbeat, release, or otherwise mutate state.
- [x] #5 Automated tests cover free/active/expired resources, unresolved started operations, release metadata, deterministic JSON and text projections, sentinel token/receipt redaction, malformed input, and the absence of side effects.
- [x] #6 README and workflow documentation define the verbose fields, redaction rules, unknown-outcome interpretation, and safe reacquisition flow; they explicitly state that `reclaimed` is reported by acquire only and is not historical diagnostic state.
- [x] #7 The implementation reuses the existing SQLite schema without a provider checkpoint API or new provider/network dependency, and focused lint, typecheck, format, and test checks pass.
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

Implementation checkpoint (T1 redacted diagnostic projection): commit a62d274ccec82b7360203e39289727fd5d5b2141. Added LeaseStore.status_verbose over existing SQLite tables with schemaVersion 1, allowlisted claim/unknown/release metadata, deterministic ordering, single-clock state classification, token/receipt redaction, and no provider writes. Verification: mise run lint; mise run format-check; mise run typecheck; mise run test (full suite passed); mise run hooks (passed). Next task: T2 deterministic JSON/text CLI and tests. Remaining acceptance: AC1-AC7; T2/T3 pending.

Implementation pass T2 claimed under claim F0796DD5-4912-4FED-BAB4-57E0A5872B74; canonical in-progress checkpoint.

Implementation pass T2 claimed under claim 723CF642-AD4A-4F15-B17C-86FF0270531F; canonical in-progress checkpoint.

Implementation checkpoint (T2 deterministic JSON/text CLI and tests): commit 7a88d9983e7a4122d26637ff7f4249e8297add77. Added status --verbose dispatch, redacted deterministic JSON/text projection, control-character-safe text fields, and CLI redaction tests. Verification: mise run lint (passed); mise run format-check (passed); mise run typecheck (passed); mise run test (full suite passed); mise run hooks (passed). Next task: T3 operator guidance. Remaining acceptance: AC6-AC7; review evidence.

Correction: claim F0796DD5-4912-4FED-BAB4-57E0A5872B74 used the non-canonical root locator and was released before isolation; canonical pass ownership is claim 723CF642-AD4A-4F15-B17C-86FF0270531F.

T2 verifier found a valid reused-operation-ID diagnostic bug; reacquired under claim 5DD1C4F0-D8D8-4084-B0D3-1BA4D2AB38E6 to fix it before handoff.

T2 verifier follow-up fixed reused-operation-ID and cross-claim reconciliation matching in commit 212b690a5fbcd0c726d1e449884f01f775f74018 (request-fingerprint matching plus deterministic claim/kind tie-break ordering; regression tests added). Final verification: mise run lint (passed); mise run format-check (passed); mise run typecheck (passed); mise run test (full suite passed); mise run hooks (passed with staged final diff; post-commit clean-tree run skipped hooks). Independent verifier rerun criteria addressed by fingerprint and cross-claim tests. Next task: T3 operator guidance. Remaining acceptance: AC6-AC7; review evidence.

Final correction: commit 792e65646e29fedde60b1af58491b549d24f0a23 adds reconciliation-kind matching and regression coverage for same-request/different-kind operations. The prior 212b690 commit hash recorded above is corrected here to 212b690ea0f64fee0c355b2267b64604b50bb62e. Final accumulated implementation commits: a62d274ccec82b7360203e39289727fd5d5b2141, 7a88d9983e7a4122d26637ff7f4249e8297add77, 212b690ea0f64fee0c355b2267b64604b50bb62e, 792e65646e29fedde60b1af58491b549d24f0a23. Final gates after kind fix: mise run lint, format-check, typecheck, test, and staged hooks all passed.

Final T2 read-only compatibility fixes: f1872b38b9424085fbef3bc23d372f32c9c86bc8 adds non-migrating SQLite reads, fresh-home and symlink guards; 949221c199f2784d71515de7f1ff61e6a1768ff7 handles legacy databases with absent tables/columns and adds legacy no-mutation coverage. Release selection now has claim_id tie-break. Exact accumulated commits: a62d274ccec82b7360203e39289727fd5d5b2141, 7a88d9983e7a4122d26637ff7f4249e8297add77, 212b690ea0f64fee0c355b2267b64604b50bb62e, 792e65646e29fedde60b1af58491b549d24f0a23, f1872b38b9424085fbef3bc23d372f32c9c86bc8, 949221c199f2784d71515de7f1ff61e6a1768ff7. Final gates exact HEAD: mise run lint, format-check, typecheck, test, and staged hooks passed; clean-tree hooks skipped checks. Next task: T3 operator guidance. Remaining acceptance AC6-AC7; review evidence.

Implementation checkpoint (T3 operator guidance): commit e9af5cf5e55c7a52c5c19f41ba4dd7dcdb2a3faa. Updated README.md and skills/worklease-workflow/SKILL.md with exact status --verbose schema fields, redaction rules, unknown-outcome handling, safe wait/reacquire guidance, acquire-only reclaimed semantics, and unchanged fencing guarantees. Verification: mise run lint (passed); mise run format-check (passed); mise run typecheck (passed); mise run test (passed); mise run hooks (passed). All refined implementation tasks complete; next pass is accumulated-item review. Remaining acceptance: review evidence.

Review marker: reviewed: implementation commits a62d274ccec82b7360203e39289727fd5d5b2141, 7a88d9983e7a4122d26637ff7f4249e8297add77, 212b690ea0f64fee0c355b2267b64604b50bb62e, 792e65646e29fedde60b1af58491b549d24f0a23, f1872b38b9424085fbef3bc23d372f32c9c86bc8, 949221c199f2784d71515de7f1ff61e6a1768ff7, e9af5cf5e55c7a52c5c19f41ba4dd7dcdb2a3faa; full implementation review clean with no valid findings. Verification: mise run lint; mise run format-check; mise run typecheck; mise run test (80 tests passed); mise run hooks (no staged files).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented and fully reviewed redacted verbose lease diagnostics across LeaseStore, CLI, tests, and operator documentation. Reviewed exact implementation commits a62d274, 7a88d99, 212b690, 792e656, f1872b3, 949221c, e9af5cf with full implementation-review depth; no valid findings. Integrated into canonical main as merge 3e8e383. Verified mise run lint, format-check, typecheck, test (99 passed), and hooks.
<!-- SECTION:FINAL_SUMMARY:END -->
