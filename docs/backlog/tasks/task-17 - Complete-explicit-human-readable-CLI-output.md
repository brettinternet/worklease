---
id: TASK-17
title: Complete explicit human-readable CLI output
status: To Do
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:46'
updated_date: '2026-07-14 03:33'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - README.md
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
<!-- SECTION:NOTES:END -->
