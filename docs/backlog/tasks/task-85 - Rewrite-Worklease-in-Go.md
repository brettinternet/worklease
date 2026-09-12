---
id: TASK-85
title: Rewrite Worklease in Go
status: To Do
assignee: []
created_date: '2026-09-12 03:21'
updated_date: '2026-09-12 03:26'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.18
references:
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
Worklease began as a Python script and the current Python package is a proof of concept with no users beyond the owner. Rebuild it as a maintainable POSIX-first Go application using patterns from the sibling hum repository, including urfave/cli v3, a typed config package, explicit process boundaries, and its dependency-light stdio MCP design. Backward compatibility with the Python API, Python SDK, plugin entry points, SQLite database, lease files, or exact CLI output is not required. Preserve the useful product capabilities and safety boundaries, simplify the interface where appropriate, and make the resulting repository suitable for unattended implementation and review loops.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A POSIX Go binary provides the agreed resource-key, lease lifecycle, bundle, guarded execution, inspection, history, maintenance, diagnostics, setup, watch, and MCP capabilities selected by the child tasks.
- [ ] #2 The production CLI and MCP server do not require Python or CGO unless a documented SQLite investigation proves CGO is the safer choice.
- [ ] #3 Python core, Python SDK, PyInstaller, and their build/release machinery are removed after their retained behavior has been replaced or explicitly rejected.
- [ ] #4 Linux amd64/arm64 and macOS amd64/arm64 release archives install and pass smoke tests.
- [ ] #5 All child tasks are complete, repository quality gates pass, and the release documentation describes the intentionally incompatible rewrite.
<!-- AC:END -->
