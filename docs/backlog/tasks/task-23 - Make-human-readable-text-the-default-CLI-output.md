---
id: TASK-23
title: Make human-readable text the default CLI output
status: In Progress
assignee:
  - '@codex-loop-fresh-20260714-worklease-pass'
created_date: '2026-07-15 02:24'
updated_date: '2026-07-15 03:21'
labels: []
dependencies: []
references:
  - TASK-17
modified_files:
  - src/worklease/cli.py
  - tests/test_cli.py
  - tests/test_package_smoke.py
  - tests/test_schemas.py
  - tests/test_gc.py
  - .github/workflows/release.yml
  - README.md
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Before the first release, make the CLI default to its stable human-readable text representation because people should receive readable output without extra flags. Keep schema-versioned JSON as an explicit automation contract through --format json and add --json as a concise equivalent. This is an intentional pre-release default change; JSON payload schemas, redaction, and exit-code semantics remain unchanged.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — Every released command, alias, parser error, operation error, and --version emits the documented text representation when no output option is supplied.
- [ ] #2 AC2 — --format json and --json both emit the existing schema-versioned JSON payloads with unchanged redaction and exit-code behavior wherever output selection is currently supported.
- [ ] #3 AC3 — Combining --json with --format is rejected deterministically as invalid input rather than using argument order to choose a format.
- [ ] #4 AC4 — CLI help, usage documentation, examples, and automated contract tests describe text as the default and require explicit JSON selection for agents and integrations.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 DOD1 — Focused CLI, parser, schema, package-smoke, and documentation contract coverage passes.
- [ ] #2 DOD2 — Full project quality gates and independent acceptance verification pass.
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
### Implementation tasks
- [x] T1 — Make CLI output selection text-first and add complete parser/renderer regression coverage (AC1-AC3). Update the existing shared output-option construction in `src/worklease/cli.py` so the root parser and command parsers default to text, retain `--format text|json`, add `--json` as an exact shorthand for JSON, and reject every `--json` plus `--format` combination even when the options straddle the command name. Update `_fallback_output_format` so parser failures follow the same contract while ignoring child argv after `--`. Extend existing CLI/package tests with default-text success and failure cases, explicit JSON through both forms and positions, aliases, `--version`, option conflicts, redaction, and child-argument isolation.
- [x] T2 — Migrate automation to explicit JSON and publish the human-first contract (AC2, AC4). Update existing JSON-parsing test helpers and release smoke commands to request JSON explicitly, keep schema validation against unchanged version-1 payloads, and revise CLI help plus `README.md` examples/reference text so interactive examples use the text default and agents/integrations use `--json` or `--format json`. Preserve all command additions present in the refreshed canonical branch.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Readiness:** available. TASK-22 is concurrently changing `src/worklease/cli.py`, `tests/test_cli.py`, and `README.md`; implementation must refresh canonical state, preserve its bundle-operation additions, and avoid treating this task as permission to overwrite those changes. TASK-22 is coordination overlap, not a logical prerequisite.

**Goal and target area:** Change only output selection and its consumers before the first release. Existing targets are `src/worklease/cli.py`, `tests/test_cli.py`, `tests/test_package_smoke.py`, `tests/test_schemas.py`, `tests/test_gc.py`, `.github/workflows/release.yml`, and `README.md`. Search the refreshed repository for any additional subprocess consumer that parses CLI stdout as JSON. No new files or schema artifacts are expected.

**Resolved decisions:** Text is the unconditional default; do not add TTY detection, environment-based switching, color, or configuration. Retain explicit `--format text`. `--json` is equivalent to `--format json`, is accepted in the same pre-command and command-local positions as `--format`, and conflicts with any simultaneous `--format` occurrence rather than allowing order-dependent precedence. Parser failures default to text, honor explicit JSON selection, and never inspect child argv after `--`. Existing schema-versioned JSON bodies, token redaction, stdout/stderr routing, and exit codes remain unchanged; this intentional pre-release default change does not require a schema-version bump.

**Evidence:** The current root parser defaults `format` to JSON, command parsers suppress their format default so the root value survives, and `_fallback_output_format` independently defaults parser errors to JSON. TASK-17 supplied deterministic text renderers for every released operation and documented text as opt-in. Current CLI, schema, package-smoke, GC, and release workflow tests parse default stdout as JSON, so they must select the automation contract explicitly. The current working state also adds bundle inspection/reconciliation operations that must remain covered.

**Task/acceptance map:** T1 → AC1-AC3; T2 → AC2 and AC4. DOD1 validates focused parser, CLI, schema, package, workflow, and documentation behavior; DOD2 applies the repository-wide gates and independent verification.

**Non-goals:** Changing JSON fields or schemas, changing exit codes, removing `--format text`, altering text grammar/renderers beyond default selection, or adding shell-completion aliases.

**Pending verification:** Default and explicit-format matrices across root/command positions, conflict permutations, aliases, parser and child failures, redaction, schema validation, native release smoke commands, full quality gates, and independent verification.

**Next action:** Implement T1 against the refreshed canonical parser and tests.

refined: TASK-23 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=C36432D3-1385-4FCF-8E8E-FDC279574BF0; claimRevision=3; refinement: complete.

T1 complete in commit 67146aa. Verification passed: mise run lint; mise run format-check; mise run typecheck; mise run test; targeted CLI/package/schema/execution/GC contract tests; direct smoke checks for default text, --json, conflicts, and child-argument isolation. Next task: T2. Remaining acceptance: AC4 and T2 automation/documentation work; AC2 coverage remains ongoing through T2.

T2 complete in commit b807255bcc86870c785297c68321cace19b23da4. Verification passed: targeted CLI/package/schema/GC contract tests; direct text and explicit JSON smoke checks; mise run lint; mise run format-check; mise run test; mise run typecheck; mise run hooks. Updated CI and release smoke commands to request --json and revised README human-first output contract. All implementation tasks are complete; next pass is REVIEW. Acceptance criteria and Definition of Done remain pending accumulated review and integration evidence.
<!-- SECTION:NOTES:END -->
