---
id: TASK-59.2
title: Add a local resource history command
status: Done
assignee:
  - '@brett'
created_date: '2026-09-07 15:00'
updated_date: '2026-09-07 17:42'
labels:
  - cli
  - docs
dependencies:
  - TASK-59.1
references:
  - src/worklease/cli.py
  - src/worklease/cli_dispatch.py
  - src/worklease/projections.py
  - src/worklease/sqlite.py
  - src/worklease/schemas/v1/common.json
  - src/worklease/schemas/v1/commands.json
  - src/worklease/schemas/v1/index.json
  - tests/test_cli.py
  - tests/test_schemas.py
  - tests/test_gc.py
  - docs/cli-reference.md
  - docs/claim-model.md
parent_task_id: TASK-59
priority: medium
type: feature
ordinal: 62000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add a read-only local `history --resource R` command that projects the retained lifecycle of one exact resource. Keep this first version resource-scoped; defer all-resource output, identity and time filters, pagination, cursors, heartbeat modes, remote-authority parity, signing, and hash chaining until a demonstrated workflow requires them.

Build the projection from acquisition epochs, explicit operation rows, reconciliations, current claim state, and epoch terminations. Acquisition is synthesized from `epochs` or `bundle_epochs`; it is not an operation. Project a bundle epoch and its synthetic-key operations under each member resource. Include every durably recorded explicit operation kind and state, but do not invent internal exec renewals that are intentionally not separate rows.

Order post-migration epochs by acquisition revision, and order their operations by expected revision with explicit stable tie breakers. Use acquired time and stable IDs only as a deterministic fallback for legacy rows whose revision is unavailable, and label those rows legacy-incomplete rather than claiming a proven ownership order. Represent termination as a separate nullable epoch field so a later reconciliation does not falsely precede an end event. A current claim with no termination is open even when its stored expiry has passed; do not derive clock-dependent active state in this deterministic history projection.

Treat reconciliation as an event of the resolver claim and expose only target claim and operation identifiers, kind, outcome, and recorded time. The target operation may show its stored or reconciled outcome, but never expose reconciliation evidence.

Construct both text and JSON from a positive field allowlist. Identity and resource strings are caller-supplied and may be sensitive. Never emit bearer tokens, token hashes, checkpoint bodies, operation or release request and receipt blobs, reconciliation evidence, argv, stdout, stderr, file contents, or provider payloads. A termination may expose checkpoint presence, not its value.

Follow existing output conventions: human-readable text by default and `--json` through a new v1 `history.json` schema registered in the command and schema indexes. Determinism requires fixed total ordering, sorted JSON keys, and no export-time timestamp or clock-derived state. Document JSON redirection as a sanitized diagnostic export; a private SQLite backup is the complete archive before `gc --apply`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `history --resource R` returns every retained singleton and bundle-member epoch for exactly R, with acquisition identity, stored times and revisions, safe explicit-operation summaries, reconciliation outcomes, and a separate end snapshot.
- [x] #2 Post-migration ownership is ordered by acquisition revision and operations by expected revision with documented stable tie breakers; legacy fallbacks are deterministic and explicitly marked legacy-incomplete.
- [x] #3 Ended, open, and legacy-incomplete epochs are distinguished without a mutating read, fabricated termination, export timestamp, or clock-derived active state; bundle operations are projected under each retained member resource.
- [x] #4 The projection uses a positive field allowlist, and tests seeded with tokens, token hashes, checkpoint secrets, argv, stdout, stderr, file contents, raw requests and receipts, and reconciliation or provider evidence prove that none appears.
- [x] #5 `history --json` validates against a new v1 history schema registered in `commands.json` and `index.json`, and repeated runs against unchanged persisted state produce byte-identical JSON.
- [x] #6 Text output and parser or runtime errors follow existing CLI conventions, with tests covering all retained operation kinds and states, singleton and bundle histories, reconciliation attribution, termination reasons, open rows, legacy gaps, ordering ties, redaction, and schema validation.
- [x] #7 `docs/cli-reference.md` documents the text grammar, local and retention boundaries, sanitized per-resource JSON export before collection, and private database backup for a complete archive; `docs/claim-model.md` states that history is local coordination metadata rather than provider evidence.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a read-only store projection that combines singleton and bundle epochs, safe operation/reconciliation summaries, current state, and epoch termination records with deterministic revision-first ordering and explicit legacy completeness.
2. Wire `history --resource` through parsing, dispatch, stable text rendering, and schema-versioned JSON output.
3. Add focused projection, redaction, ordering, text, error, determinism, and schema tests covering singleton and bundle lifecycle variants.
4. Document the command grammar, safe-export/retention/archive boundaries, and separation from provider evidence.
5. Run focused and repository quality gates, review the diff, then finalize TASK-59.2.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented `history --resource` with exact-resource singleton and bundle epoch projection, safe operation/reconciliation summaries, deterministic ordering, schema-v1 JSON, text output, release packaging, tests, and documentation. Independent review found three issues: invalid database paths returned empty history, unrelated secret-bearing rows were over-read, and the archive example mishandled an unset WORKLEASE_HOME. Fixed all three and added regression coverage.

Verification: `mise run lint`, `mise run format-check`, `mise run test` (268 core + 19 SDK), `mise run typecheck`, `mise run hooks`, and `git diff --check` passed. Independent adversarial review completed; all three findings were fixed and covered. Implementation commit f6a0437 merged to main as ca3b5f9.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added deterministic, read-only local resource history for singleton and bundle epochs with safe operation/reconciliation and termination projections, text and schema-v1 JSON output, release packaging, focused redaction/ordering/error coverage, and local/provider retention documentation. Verified with all repository quality gates and independent review; merged to main in ca3b5f9.
<!-- SECTION:FINAL_SUMMARY:END -->
