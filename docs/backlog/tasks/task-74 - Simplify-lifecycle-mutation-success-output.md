---
id: TASK-74
title: Simplify lifecycle mutation success output
status: To Do
assignee: []
created_date: '2026-09-12 02:11'
updated_date: '2026-09-12 02:17'
labels:
  - cli
  - ux
dependencies:
  - TASK-70
  - TASK-67.2
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - docs/cli-reference.md
  - docs/claim-model.md
  - scripts/release_docs.py
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 81000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Successful lifecycle commands currently emit a generic field dump containing long resources, lifecycle identifiers, guarantees, timestamps, and command-specific values. This obscures the action result and makes routine acquire, renew, checkpoint, transfer, release, and guarded execution flows hard to follow. Give each operation a concise human summary while retaining every value required for the next safe lifecycle step.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Default text output uses an operation-specific success summary for singleton and bundle acquire, heartbeat, checkpoint, transfer, release, exec, and replace-file commands rather than the generic claim field dump.
- [ ] #2 Every summary retains the resource or bundle identity, resulting revision, relevant agent/work identity, and concise lease timing or terminal result needed to understand the mutation.
- [ ] #3 Acquire and transfer emit bearer tokens only for the explicit stateless flows defined by TASK-67.2; contextual and explicit handle-file flows identify the usable handle without exposing its contents. No other success or failure path exposes bearer tokens.
- [ ] #4 Checkpoint output reports successful persistence and byte size without printing a potentially large checkpoint body by default; release output reports the terminal reason; transfer clearly identifies the successor.
- [ ] #5 Guarded exec and exec-bundle preserve return code, truncation and byte-count metadata, execution directory, and escaped stdout/stderr exactly enough to diagnose the child command; summary changes do not merge child streams or print raw control characters.
- [ ] #6 Failure output remains concise, actionable, command-specific, and redacted.
- [ ] #7 CLI contract tests cover every canonical singleton and bundle mutation, aliases where relevant, contextual-handle, explicit-handle, and stateless token flows, child success/failure/truncation, empty optional fields, full-detail behavior, and JSON compatibility.
- [ ] #8 A consistent documented `--full` text option on canonical singleton and bundle mutation commands, and their aliases, preserves all current non-secret text fields; JSON schemas and payloads remain compatible, command help and generated release documentation render successfully, and CHANGELOG `Unreleased` records the changed text output.
<!-- AC:END -->
