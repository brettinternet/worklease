---
id: TASK-7
title: Add sanitized verbose lease diagnostics
status: To Do
assignee: []
created_date: '2026-07-14 02:14'
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
