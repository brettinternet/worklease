---
id: TASK-17
title: Complete explicit human-readable CLI output
status: Blocked
assignee:
  - '@codex-loop-fresh-20260714-worklease-pass'
created_date: '2026-07-14 02:46'
updated_date: '2026-07-15 00:08'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - README.md
modified_files:
  - src/worklease/cli.py
priority: medium
type: enhancement
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Keep schema-versioned JSON as the default for agents and integrations. Make --format text a consistently human-readable opt-in mode across every CLI command instead of falling back to compact JSON for most operations.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 JSON remains the default for every command and its released schema and exit-code behavior remain compatible.
- [ ] #2 Every command has a documented, deterministic --format text representation that does not emit compact JSON as a fallback.
- [ ] #3 Text status and list output never expose bearer tokens; mutation output exposes only the minimum owner data needed for continued operation.
- [ ] #4 Tests cover successful and failed text output for every command, including child-process failures and parser errors.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Command-specific text renderers (AC1-AC3).** Replace the compact-JSON text fallback with an explicit renderer registry covering every command released at implementation time. Keep JSON envelopes and exit codes unchanged; use deterministic field order/tables, emit only the minimum owner response (token only where the successful owner must retain it), and keep status/list and all failures redacted.
2. **[T2] Complete text contract tests (AC2-AC4).** Add golden behavioral tests for success and failure of version, key/policy commands, acquire, status/list, heartbeat/checkpoint, release, exec, replace-file, and any then-released bundle/reconciliation/GC commands. Cover parser errors, child failure/stdout/stderr sections, empty lists, Unicode/opaque values, and sentinel secrets without asserting JSON source text.
3. **[T3] Human-readable output reference (AC2-AC4).** Document the stable text grammar and examples per command, the stdout/stderr and escaping rules, fields intentionally omitted for security, and that automation must continue using default JSON.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now.

**Goal and target area:** Refactor text rendering in existing `src/worklease/cli.py`, extend CLI tests, and document the opt-in text grammar in README. Do not change JSON payload construction.

**Resolved decisions:** Default JSON, schema version, fields, and exit codes are frozen. Text mode uses an explicit renderer per released operation; no command may fall back to compact JSON. Successful owner mutations show only values needed for the next lifecycle step, including a token only for acquire/heartbeat/checkpoint responses that intentionally return one. Read-only commands and failures never show tokens. Deterministic tables/ordered key-value lines use UTF-8, literal values with escaped control characters, and explicit labeled stdout/stderr blocks for guarded child results. Parser/operation errors render `ERROR <operation>: <reason>` plus allowlisted details on stdout, preserving the current output stream and exit status. Automation continues to use JSON.

**Non-goals:** changing JSON, localization/color, interactive prompts, provider-specific prose, or stabilizing terminal width/layout beyond documented line grammar.

**Evidence and assumptions:** Current text handles version/list and otherwise dumps compact JSON. The command set may grow before TASK-17; implementation must snapshot and cover every released command then present.

**Task/acceptance map:** T1→AC1-3; T2→AC2-4; T3→AC2-4.

**Pending verification:** golden behavioral tests for every command success/failure, parser and child failures, Unicode/control escaping, empty output, sentinel redaction, full quality gates.

**Next action:** inventory response payloads and implement the renderer registry without touching dispatch semantics.

**Refinement checkpoint:** refined: TASK-17 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=2ee49790-a2f4-4772-b50a-d9533e283bbc; claimRevision=3; refinement: complete.

Implementation checkpoint (T1): commit 29e8a2082eb15bcf0b2ef6702ceb4bc44b0330cc adds an explicit deterministic text-renderer registry for every released CLI command and aliases, with redacted read-only output and minimum owner fields for lifecycle mutations. Files: src/worklease/cli.py. Verification: mise run lint passed; mise run format-check passed; mise run test passed (127 tests); mise run typecheck passed; mise run hooks passed. Next task: T2 complete text contract tests. Remaining acceptance criteria: #1-#4.

Implementation checkpoint (T2): commit db0bf2d6a7d39ba060bf7a9f9bf1465f0ad9fba5 adds complete human-readable text contract coverage for released CLI commands, aliases, parser/child failures, redaction, escaping, free status identity, and GC. Files: src/worklease/cli.py, tests/test_cli.py. Verification: focused CLI tests (17 passed); mise run lint passed; mise run format-check passed; mise run test passed; mise run typecheck passed; mise run hooks passed. Next task: T3 human-readable output reference. Remaining acceptance criteria: #1-#4.

T2 verification follow-up claimed: independent verifier found AC4 coverage gaps for aliases and per-command failures; add only missing text contract probes under this T2 ownership epoch.

T2 coverage fix complete: commit 00835854dfa866c5168352c1466b5a4db8db8f9f adds canonical bundle text success probes, inspect-bundle, empty-list output, and parser-error coverage for every released command and alias. Verification: focused CLI tests (19 passed); mise run lint, format-check, test, typecheck, and hooks passed. Independent verifier rerun pending. Next task remains T3 human-readable output reference; implementation tasks T1-T2 complete.

T2 control-escaping fix: commit af414e469c451962a639ac6292998106be8ff56c escapes all C0/DEL control characters in text atoms and adds isolated ESC regression coverage. Independent verifier finding resolved. Final T2 verification: focused CLI tests 19/19; mise run lint, format-check, test, typecheck, and hooks passed. Next task: T3 human-readable output reference; T2 implementation is complete.

Final T2 fixes: commit df80acf743486afc521e28d44afe0316f8490b3 centralizes C0/C1/DEL escaping in _text_value, adds DEL regression coverage, and preserves bundle resources in text list output. Final gates passed: mise run lint, format-check, test, typecheck, and hooks; focused CLI tests 19/19. Independent verification rerun after the fixes is required before release.

Independent verifier PASS after final fixes: 19/19 focused CLI tests; JSON compatibility, renderer registry/aliases, C0/C1/DEL and Unicode escaping, bundle-resource list identity/redaction, parser and child failures all pass. No remaining T2 defect. T3 human-readable output reference remains next.

BLOCKED (2026-07-15T00:08Z): implementation loop halted before coding because the item lacks the canonical ### Implementation tasks section with direct [ ]/[x] task entries required by backlog-source-workflow; the plan's [T1]-[T3] entries are not a claimable checklist. Evidence: backlog task view TASK-17 shows only Implementation Plan and prior notes; no canonical checklist. Attempt: refreshed provider state, acquired and released an implementation claim without edits. Unblock condition: run backlog-refine for TASK-17 to add a canonical ordered implementation-task checklist, then resume with @docs/backlog.
<!-- SECTION:NOTES:END -->
