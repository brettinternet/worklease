---
id: TASK-73
title: Add concise timeline output for history
status: To Do
assignee: []
created_date: '2026-09-12 02:11'
updated_date: '2026-09-12 02:17'
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
- [ ] #1 Default text output begins with one concise resource label and a coverage summary that plainly identifies complete, open, legacy-incomplete, and retention-bounded history without implying provider audit completeness.
- [ ] #2 Each epoch is rendered as a compact timeline entry showing acquisition time or age, agent, work key, state or termination, and operation/reconciliation counts without repeating the resource or printing claim, session, and owner IDs by default.
- [ ] #3 Operation and reconciliation entries use labeled or otherwise self-explanatory fields rather than undocumented positional columns, and their ordering remains deterministic.
- [ ] #4 Current-claim and termination information is summarized without treating a retained snapshot as proof that a claim is still active.
- [ ] #5 A documented `--full` text mode preserves every redacted field and timestamp currently available, including source provenance, revisions, identifiers, completeness, operations, reconciliations, termination, and current snapshots.
- [ ] #6 JSON output remains schema-compatible and complete.
- [ ] #7 CLI and history tests use fixed timestamps and cover complete, open, legacy-incomplete, retention-bounded, empty, bundle, operation, reconciliation, termination, current-snapshot, full, and JSON cases; command help and documentation explain the summary semantics, generated release documentation renders successfully, and CHANGELOG `Unreleased` records the changed text output.
<!-- AC:END -->
