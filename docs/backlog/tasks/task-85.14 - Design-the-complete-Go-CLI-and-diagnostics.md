---
id: TASK-85.14
title: 'Complete the CLI, diagnostics, and agent instructions'
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 06:07'
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
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 106000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Complete coherent CLI help, errors, diagnostics and canonical agent instructions from contract sections 4–6 and 13. Own internal/cli polish, internal/doctor and internal/instructions. Reuse existing command and output patterns; do not create aliases or parallel configuration paths.

At this dependency stage MCP/setup may not yet be registered. Validate the commands available now against the specified subset, and provide an expectation list that 85.15/85.16 extend; the final full-tree check belongs after those tasks. Doctor reports authority/config/session mismatch and clock uncertainty read-only. Explain one-loop session selection, exact resource coverage, pending-request recovery and honest local guarantees.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Surface/help tests validate all commands available at this stage, exact flags/short options and examples; extension tests cover later registration without requiring unfinished MCP/setup commands.
- [ ] #2 Black-box tests exercise success and applicable failure families in text/JSON, exclusive selectors, config precedence and redacted committed/unknown failures.
- [ ] #3 Ergonomics tests cover inline provider/path acquisition, agent defaults, stable session selectors, optional release reason, holder metadata and expiry-aware watch guidance.
- [ ] #4 Doctor reports configuration sources, authority identity, missing/unsafe state, clock regression, Git/context/session and Python-era leftovers without creating/chmodding state or exposing private content.
- [ ] #5 Canonical instructions distinguish task/path resources, claim/operation/provider state, pending recovery and unfenced native/provider effects; mise run ci-go passes.
- [ ] #6 From an empty isolated home with no config, setup or explicit credentials, black-box tests run acquire --path, contextual status, exec and release in both human and --json modes; verify concise actionable contention output, exactly one machine envelope without prompts/logs, and two loops isolated by session environment alone.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
