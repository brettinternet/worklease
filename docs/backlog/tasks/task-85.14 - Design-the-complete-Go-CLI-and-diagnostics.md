---
id: TASK-85.14
title: 'Complete the CLI, diagnostics, and agent instructions'
status: Done
assignee:
  - '@pi-01a09679'
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 18:42'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.11
  - TASK-85.12
  - TASK-85.13
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/instructions.py
  - tests/test_cli.py
  - ../hum/internal/cli/help_contract_test.go
  - ../hum/internal/cli/surface_test.go
  - ../hum/internal/cli/man.go
  - TASK-74
  - TASK-75
  - TASK-76
  - TASK-77
  - TASK-78
  - TASK-83
modified_files:
  - CHANGELOG.md
  - internal/cli/commands.go
  - internal/cli/doctor_commands.go
  - internal/cli/doctor_commands_test.go
  - internal/cli/lease_commands.go
  - internal/cli/resource_commands_test.go
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/watch_commands.go
  - internal/cli/watch_commands_test.go
  - internal/doctor/doctor.go
  - internal/doctor/doctor_test.go
  - internal/handle/handle.go
  - internal/handle/handle_test.go
  - internal/instructions/instructions.go
  - internal/output/output.go
  - internal/output/output_test.go
  - internal/store/store.go
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 106000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Complete coherent CLI help, errors, diagnostics and canonical agent instructions from contract sections 4–6 and 13. Own internal/cli polish, internal/doctor and internal/instructions. Reuse existing command and output patterns; do not create aliases or parallel configuration paths.

At this dependency stage MCP/setup may not yet be registered. Validate the commands available now against the specified subset, and provide an expectation list that 85.15/85.16 extend; the final full-tree check belongs after those tasks. Doctor reports authority/config/session mismatch and clock uncertainty read-only. Explain one-loop session selection, exact resource coverage, pending-request recovery and honest local guarantees.

Evidence and patterns (the amended contract is normative): `src/worklease/instructions.py` (canonical loop and safety text to adapt). Tests in `tests/test_cli.py`: test_help_groups_commands_and_shows_common_examples, test_help_examples_cover_every_canonical_command, test_help_documents_lease_defaults, test_actionable_parser_hints_preserve_json_and_redact_values, test_no_arguments_show_help_and_invalid_commands_fail, test_text_parser_errors_cover_every_command_and_alias. Closed tasks TASK-74 through TASK-78 and TASK-83 describe the ergonomics and doctor intent. hum patterns: `internal/cli/help_contract_test.go`, `surface_test.go`, `ergonomics_test.go`, `json_errors_test.go`, and `internal/cli/man.go` (help text must be man-page friendly for TASK-85.17).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Surface/help tests validate all commands available at this stage, exact flags/short options and examples; extension tests cover later registration without requiring unfinished MCP/setup commands.
- [x] #2 Black-box tests exercise success and applicable failure families in text/JSON, exclusive selectors, config precedence and redacted committed/unknown failures.
- [x] #3 Ergonomics tests cover inline provider/path acquisition, agent defaults, stable session selectors, optional release reason, holder metadata and expiry-aware watch guidance.
- [x] #4 Doctor reports configuration sources, authority identity, missing/unsafe state, clock regression, Git/context/session and Python-era leftovers without creating/chmodding state or exposing private content.
- [x] #5 Canonical instructions distinguish task/path resources, claim/operation/provider state, pending recovery and unfenced native/provider effects; mise run ci-go passes.
- [x] #6 From an empty isolated home with no config, setup or explicit credentials, black-box tests run acquire --path, contextual status, exec and release in both human and --json modes; verify concise actionable contention output, exactly one machine envelope without prompts/logs, and two loops isolated by session environment alone.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add canonical Go loop/safety instruction text and wire real instructions CLI actions.
2. Add a strictly read-only doctor package and CLI projection covering configuration, authority, context/session, state safety, clock, Git, MCP availability, and Python-era leftovers.
3. Strengthen staged command-tree/help and black-box ergonomics coverage for current commands, JSON/text errors, config/selection rules, contention, optional release reason, and session-isolated empty-home lifecycles.
4. Run focused Go tests, review the diff, then run all repository and ci-go quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in 5189a17 and merged to main in the subsequent merge commit. Independent review found and drove fixes for unsafe ancestry checks, grouped JSON help, clock tolerance, command help, and missing acceptance evidence. Final validation passed: mise run lint, mise run format-check, mise run test, mise run typecheck, and post-merge mise run ci-go.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed Go CLI diagnostics, canonical instructions, staged help, redacted error reporting, and setup-free lifecycle ergonomics. AC1: TestCommandTreeRegistrationHelpAndShortOptions and TestCanonicalCommandHelpPathsFlagsAndExamples verify the staged surface, exact flags/aliases, usage paths, and examples. AC2: TestCLIConfigPrecedenceAndBlankEnvironment, TestRunAcquireCommittedHandleWriteFailureIsRedacted, TestRunStartedOperationRetryReportsUnknownOutcomeRedacted, and existing selector/parser tests verify precedence, exclusivity, failure families, and redaction. AC3: TestSetupFreePathLifecycleTextAndJSON, TestContextualLoopsUseSessionEnvironmentOnly, TestWriteTextErrorIncludesSafeHolderMetadata, and TestWatchTextIncludesExpiryGuidance verify ergonomic defaults and guidance. AC4: internal/doctor tests plus TestDoctorUnsafeStateFailureEnvelopeAndHints and TestDoctorDoesNotExposePrivateHandleOrPythonState verify all read-only checks, unsafe state, clock/context failures, identity, leftovers, and secrecy. AC5: TestInstructionsAndDoctorAreReadOnly verifies canonical loop/safety text; post-merge mise run ci-go passed. AC6: TestSetupFreePathLifecycleTextAndJSON and TestContextualLoopsUseSessionEnvironmentOnly verify empty-home human/JSON lifecycle, one-envelope output, contention guidance, and session-only isolation. All repository gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
