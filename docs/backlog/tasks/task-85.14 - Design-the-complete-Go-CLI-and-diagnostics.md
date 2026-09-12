---
id: TASK-85.14
title: Design the complete Go CLI and diagnostics
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
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
  - src/worklease/cli.py
  - src/worklease/cli_dispatch.py
  - ../hum/internal/cli
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
Turn the underlying Go capabilities into one coherent urfave/cli v3 interface rather than cloning the proof-of-concept parser. Make ordinary human and agent workflows path-free and concise, keep machine output dependable, and integrate the worthwhile ergonomics from TASK-74 through TASK-78 plus the environment diagnostics from TASK-83. Configuration may come from flags, environment, and optional YAML according to the product contract.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The command tree exposes all retained lifecycle, bundle, key/policy, execution, verification, inspection, history/events/watch, maintenance, diagnostics, setup, and MCP entry points with consistent names and help.
- [ ] #2 Routine lifecycle commands use contextual handles, default agent identity safely, derive provider resources inline, make boilerplate release metadata optional, and provide actionable contention holder/wait guidance without exposing secrets.
- [ ] #3 Human output is concise and operation-specific; full diagnostic output is explicit; machine output has a documented version, typed errors, deterministic field semantics, and no partial success ambiguity.
- [ ] #4 A read-only doctor command reports configuration sources, resolved authority/context, permissions, identity, database health, MCP availability, and clock assumptions while distinguishing facts, warnings, and unknowns and creating no state.
- [ ] #5 Black-box tests cover command discovery, help, aliases if any, configuration precedence, every success and failure family, output modes, stdin/TTY behavior, diagnostics, and redaction.
<!-- AC:END -->
