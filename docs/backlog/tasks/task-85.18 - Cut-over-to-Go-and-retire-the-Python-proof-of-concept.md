---
id: TASK-85.18
title: Cut over to Go and retire the Python proof of concept
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.17
references:
  - pyproject.toml
  - uv.lock
  - src/worklease
  - tests
  - packages/worklease-source-sdk
  - TASK-67
  - TASK-74
  - TASK-75
  - TASK-76
  - TASK-77
  - TASK-78
  - TASK-79
  - TASK-80
  - TASK-81
  - TASK-82
  - TASK-83
  - TASK-84
parent_task_id: TASK-85
priority: high
type: chore
ordinal: 110000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Complete the intentionally incompatible rewrite after the Go implementation and release path are independently verified. Remove the Python core, source-provider SDK, PyInstaller and uv packaging, obsolete tests and generated artifacts, and resolve the legacy Python backlog so unattended loops cannot select duplicate proof-of-concept work.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Production Python sources, the source-provider SDK and example, Python-only tests and tooling, uv packaging, and PyInstaller release paths are removed; retained non-production scripts have an explicit continuing purpose.
- [ ] #2 The default worklease and MCP entry points execute the Go implementation, use a fresh Go authority, and pass a clean-checkout end-to-end lifecycle including bundles, guarded execution, history/events/watch, maintenance, diagnostics, and setup.
- [ ] #3 Every nonterminal Python-era task overlapping TASK-85 is updated through Backlog.md as superseded, rewritten for the Go design, or retained with a specific non-duplicate rationale; unattended go-rewrite selection sees no ambiguous duplicate work.
- [ ] #4 An independent review targets filesystem safety, process cleanup, transaction boundaries, secret redaction, goroutine leaks, and release rollback; every blocking finding is resolved.
- [ ] #5 All project quality, clean-tree build, archive installation, CLI, MCP, and documentation checks pass with no Python installation present.
<!-- AC:END -->
