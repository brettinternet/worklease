---
id: TASK-85
title: Rewrite Worklease in Go
status: To Do
assignee: []
created_date: '2026-09-12 03:21'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.18
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - ../hum/cmd/hum/main.go
  - ../hum/internal/config/config.go
  - ../hum/internal/cli/root.go
  - ../hum/internal/mcp/server.go
  - src/worklease
  - tests
priority: high
type: feature
ordinal: 92000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Worklease began as a Python script and the current Python package is a proof of concept with no users beyond the owner. Rebuild it as a maintainable POSIX-first Go application using patterns from the sibling hum repository (urfave/cli v3, a typed config package, explicit process boundaries, dependency-light stdio MCP). Backward compatibility with the Python API, SDK, plugin entry points, SQLite database, lease files, or exact CLI output is not required. Preserve the useful capabilities and safety boundaries, simplify the interface, and make the repository suitable for unattended implementation and review loops.

The design is fixed in the Go Product Contract at `docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md`. Every subtask names the contract sections it implements, the Python files that are behavior evidence, the hum files that are patterns, and the paths it owns. Agents must not reopen fixed decisions; contract section 15 defines the amendment procedure and section 19 describes how to work the plan unattended, including the parallel waves.

Unattended execution: select only tasks labeled `go-rewrite` whose dependencies are Done. This parent task is the closure task: it becomes actionable only after TASK-85.18 is Done and consists of re-verifying the acceptance criteria below with evidence and recording the final summary. Python-era tasks (TASK-67, TASK-74 through TASK-84) are resolved by TASK-85.18, not here.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A POSIX Go binary provides every command in contract section 4 and every MCP tool in contract section 12, verified by the surface tests of TASK-85.14 and TASK-85.15 and the end-to-end script of TASK-85.18.
- [ ] #2 The production CLI and MCP server require neither Python nor CGO, or the contract section 17 amendment from TASK-85.4 documents why CGO is required; `CGO_ENABLED=0 go build ./...` (or the amended command) succeeds for all four targets.
- [ ] #3 Python core, Python SDK, PyInstaller, uv packaging, and their build and release machinery are removed, and the capability inventory from TASK-85.1 shows every retained row delivered by a Done task or explicitly rejected with rationale.
- [ ] #4 Linux amd64 and arm64 and macOS amd64 and arm64 release archives install and pass the smoke tests in the release workflow for the v1.0.0 tag or a dispatch run against it.
- [ ] #5 All eighteen child tasks are Done with final summaries, `mise run ci` passes on the final commit, and CHANGELOG.md describes the intentionally incompatible rewrite and the Python-era state disposal instructions.
<!-- AC:END -->
