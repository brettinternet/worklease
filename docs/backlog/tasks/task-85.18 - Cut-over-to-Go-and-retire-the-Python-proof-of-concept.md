---
id: TASK-85.18
title: Cut over to Go and retire the Python proof of concept
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 05:51'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.17
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - pyproject.toml
  - uv.lock
  - src/worklease
  - tests
  - packages/worklease-source-sdk
  - scripts
  - mise.toml
  - .github/workflows/ci.yml
  - CLAUDE.md
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
Retire the Python proof of concept after the Go capability inventory is satisfied and artifacts are validated. Own Python-era removal, Go generic quality gates/hooks and final integration/review. Read contract sections 14–16 and 19–21; preserve historical backlog records and the deferred remote proposal.

Remove the exact tracked Python core/SDK/tests/packaging/build assets using recoverable cleanup. Repoint lint/format/test/typecheck/ci to Go and update AGENTS/CLAUDE guidance, preserving managed blocks. Historical Python references are allowed in committed task evidence and migration notes; do not erase evidence to satisfy a broad grep.

Verify duplicate Python-era tasks stay superseded. Run clean Go-only builds and native end-to-end scenarios, obtain independent review of filesystem safety, process cleanup, transaction/replay/recovery boundaries, redaction and cancellation, and resolve blocking findings. Release publishing remains a separately authorized action.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Tracked Python runtime/SDK/build/test assets and Python mise/CI jobs are retired; historical backlog/migration evidence and docs/distributed-cloudflare-claim-authority.md remain, with no remote implementation.
- [ ] #2 Generic repository gates and hooks pass with Python unavailable to the build/test commands, and ci includes Go race/smoke checks without bypassing installed hooks.
- [ ] #3 A committed clean-checkout end-to-end script covers multi-resource/session claims, path-covered replacement, supervised exec, pending recovery, predecessor reconciliation, history/events/watch/GC/doctor/setup and MCP on Linux/macOS.
- [ ] #4 Every retained/redesigned inventory capability is delivered or explicitly rejected with rationale; no duplicate Python-era task remains selectable, and the full command/tool surface is verified.
- [ ] #5 An independent review report records concrete findings and resolution evidence; TASK-85 receives final acceptance evidence for code/artifacts without requiring unauthorized publication.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes in a clean checkout with no Python on PATH
- [ ] #2 Final summary names the commands and review report that prove each acceptance criterion
<!-- DOD:END -->
