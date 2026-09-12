---
id: TASK-85.2
title: Bootstrap the Go application and typed configuration
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.1
references:
  - ../hum/cmd/hum/main.go
  - ../hum/internal/config/config.go
  - ../hum/internal/cli/root.go
  - ../hum/go.mod
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 94000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Create the production Go foundation using the owner’s preferred patterns from hum: a small signal-aware main package, urfave/cli v3 command construction, a framework-independent typed config package, injected writers and build metadata, and explicit error-to-exit handling. This foundation must be useful to every later slice without importing Python-era structure wholesale.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The repository has a Go module and a worklease command that builds and reports injected version/build metadata on Linux and macOS.
- [ ] #2 A typed config package resolves documented defaults, command flags, WORKLEASE-prefixed environment variables, XDG values, and an optional YAML config source with deterministic precedence and validation.
- [ ] #3 The urfave/cli v3 root exposes a maintainable command-group skeleton, injected stdout/stderr, context cancellation for POSIX termination signals, and typed exit errors.
- [ ] #4 Go formatting, vetting, unit tests, race tests where supported, and dependency checks are available through mise and CI without removing still-needed transition checks.
- [ ] #5 Tests cover config precedence, malformed YAML, invalid values, signal cancellation, output routing, and exit-code propagation.
<!-- AC:END -->
