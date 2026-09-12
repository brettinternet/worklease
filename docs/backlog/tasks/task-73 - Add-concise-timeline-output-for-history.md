---
id: TASK-73
title: Add concise timeline output for history
status: Done
assignee:
  - '@pi-01a0938d'
created_date: '2026-09-12 02:11'
updated_date: '2026-09-12 03:20'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - tests/test_history.py
  - docs/cli-reference.md
  - docs/claim-model.md
  - scripts/release_docs.py
  - CHANGELOG.md
modified_files:
  - src/worklease/cli.py
  - tests/test_cli.py
  - tests/test_history.py
  - docs/cli-reference.md
  - docs/claim-model.md
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 80000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Human-readable `history` currently prints a diagnostic field dump for every epoch, repeats long resource and lifecycle identifiers, and renders operations as unlabeled positional columns. It is difficult to scan the lifecycle of a resource or distinguish complete history from retention and migration gaps. Provide a chronological human summary while preserving forensic detail on demand.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Default text output begins with one concise resource label and a coverage summary that plainly identifies complete, open, legacy-incomplete, and retention-bounded history without implying provider audit completeness.
- [x] #2 Each epoch is rendered as a compact timeline entry showing acquisition time or age, agent, work key, state or termination, and operation/reconciliation counts without repeating the resource or printing claim, session, and owner IDs by default.
- [x] #3 Operation and reconciliation entries use labeled or otherwise self-explanatory fields rather than undocumented positional columns, and their ordering remains deterministic.
- [x] #4 Current-claim and termination information is summarized without treating a retained snapshot as proof that a claim is still active.
- [x] #5 A documented `--full` text mode preserves every redacted field and timestamp currently available, including source provenance, revisions, identifiers, completeness, operations, reconciliations, termination, and current snapshots.
- [x] #6 JSON output remains schema-compatible and complete.
- [x] #7 CLI and history tests use fixed timestamps and cover complete, open, legacy-incomplete, retention-bounded, empty, bundle, operation, reconciliation, termination, current-snapshot, full, and JSON cases; command help and documentation explain the summary semantics, generated release documentation renders successfully, and CHANGELOG `Unreleased` records the changed text output.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a compact chronological history renderer with clear coverage wording, concise epoch summaries, labeled operation/reconciliation lines, and non-authoritative current-snapshot wording.
2. Add history --full to preserve the existing redacted diagnostic rendering while leaving JSON unchanged.
3. Expand fixed-time CLI/history tests for coverage states, singleton/bundle epochs, operations, reconciliations, terminations, current snapshots, full mode, and JSON compatibility.
4. Update command help, CLI/claim-model docs, generated release docs, and CHANGELOG; run all repository quality gates and review the diff.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented compact chronological history text, preserved the prior full diagnostic projection behind --full, added rich fixed-timestamp renderer and CLI coverage, and updated CLI/claim-model documentation plus changelog. Focused history and CLI suites pass (99 tests).

Independent review found one compact-rendering defect: a terminated migration-era epoch hid legacy-incomplete completeness behind its release reason. Fixed by keeping completeness in STATE and the termination reason on its labeled row; added a regression fixture contrasting complete and terminated legacy-incomplete epochs. After rebasing onto current main (including TASK-69 events and TASK-72 GC output), mise run lint, format-check, test (324 core + 19 SDK), and typecheck all pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added concise chronological history text with compact resources, explicit retention/provider-audit caveats, per-epoch completeness, labeled nested records, and non-authoritative current snapshots. Added --full for the complete prior redacted projection while preserving JSON. Verified with focused CLI/history tests, independent review, generated-release tests, and all repository quality gates after rebasing onto main.
<!-- SECTION:FINAL_SUMMARY:END -->
