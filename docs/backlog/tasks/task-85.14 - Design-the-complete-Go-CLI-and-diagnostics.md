---
id: TASK-85.14
title: 'Complete the CLI, diagnostics, and agent instructions'
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.5
  - TASK-85.10
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
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 106000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
By this point every capability exists as a command added by its owning task. This task makes the whole tree one coherent product: consistent names, help, no aliases, safe defaults, error hints, contention guidance, provider-input wiring on `acquire`, a read-only `doctor` (the TASK-83 intent), canonical `instructions`, and black-box coverage of every success and failure family. It also lands the ergonomics that TASK-74 through TASK-78 asked for and that the contract already fixed: agent default from the OS user, optional release reason, inline provider input, holder details on contention.

Read first: contract sections 4 (the entire tree and the short-option table), 5, 6, 13 (doctor and instructions), 19. Python evidence: `src/worklease/instructions.py`; `tests/test_cli.py`: test_help_groups_commands_and_shows_common_examples, test_help_examples_cover_every_canonical_command, test_help_documents_lease_defaults, test_actionable_parser_hints_preserve_json_and_redact_values, test_no_arguments_show_help_and_invalid_commands_fail, test_text_parser_errors_cover_every_command_and_alias; the TASK-83 acceptance criteria and the TASK-74, TASK-75, TASK-76, TASK-77, TASK-78 descriptions. Patterns: `../hum/internal/cli/help_contract_test.go`, `surface_test.go`, `ergonomics_test.go`, `json_errors_test.go`, and `../hum/internal/cli/man.go` (help text must be man-page friendly for TASK-85.17).

Deliver: an audit of `internal/cli` with fixes so every command in contract section 4 exists with the exact names, flags, and short options; `acquire` accepts the provider triple and `--path` through the TASK-85.5 `ResourceInput` group and derives inline; agent id defaults per contract 5 with an agent-id-required hint naming `--agent`, `WORKLEASE_AGENT_ID`, and the YAML key; optional release reason; contention output showing holder claimId, agentId, workKey, expiresAt and a `watch -r R --until free` hint; usage errors carrying a safe hint that never echoes rejected values; an example in every command's `--help`; a surface test enumerating the tree against a golden list. `internal/doctor` and the `doctor` command per contract 13 (read-only, check ids, statuses, hints, exit code). `internal/instructions` with `instructions loop|safety` text adapted to the Go command names. A review of every command's text output for one-line summaries and stable `key: value` ordering.

Owned paths: `internal/cli` (all files, coordination polish), `internal/doctor`, `internal/instructions`, `internal/cli/doctor.go`, `instructions.go`. Out of scope: MCP, setup generators, documentation files (TASK-85.17).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A surface test proves the command tree, flag names, and short options exactly match contract section 4 through a golden list, every command's --help includes an example and env-var references where applicable, and no aliases exist.
- [ ] #2 Black-box tests cover for every command at least one success and each failure family it can produce (2, 3, 64, 75, 124 where applicable) in text and JSON, plus configuration precedence through flags, environment, and a YAML file for home, agent id, ttl, and poll interval.
- [ ] #3 Ergonomics tests prove `acquire -p backlog-md -s docs/backlog -i TASK-1` derives and acquires in one invocation showing the derived resource, acquire without any agent source and without an OS user (injected) fails agent-id-required with the three-way hint, release without --reason records reason released, and contention output includes holder metadata and the watch hint but never a token.
- [ ] #4 doctor tests cover healthy, missing home, insecure home permissions, foreign-owned handle, split authority (WORKLEASE_HOME differs from the YAML home), linked worktree context, unreadable database, missing git, and unknown checks; the command creates no files (directory listing unchanged), never opens the database read-write, exits 1 only when a check is fail, and its JSON lists every check id from contract 13.
- [ ] #5 `instructions loop` and `instructions safety` output is golden-tested and references only Go command names; tests prove JSON mode never prompts on a TTY and non-UTF-8 arguments fail invalid-argument; `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
