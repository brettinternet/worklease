---
id: TASK-85.2
title: Bootstrap the Go application and typed configuration
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 05:51'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.1
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - ../hum/cmd/hum/main.go
  - ../hum/internal/config/config.go
  - ../hum/internal/cli/root.go
  - ../hum/mise.toml
  - ../hum/.taskfiles/cli.yaml
  - ../hum/.github/workflows/ci.yaml
  - src/worklease/cli.py
  - tests/test_cli.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 94000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Bootstrap the Go binary and shared config/error/output foundation without lease behavior. Read contract sections 2–6, 14, 18 and 20. Reuse the cited hum entry point, urfave/cli v3, typed config and injected writer patterns where they fit this product. The capability inventory is a prerequisite.

Own go.mod/go.sum, cmd/worklease, internal/config, internal/reason, internal/output, the internal/cli skeleton, and additive Go mise/CI jobs. Pin Go 1.27.1 and runtime dependencies; retain Python tooling until cutover. Configuration includes stable session selection, explicit home/config sources, and the exclusive selector boundary. Register every amended reason and support redacted committed/unknown error results. Do not create an unused remote interface or backend registry.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The pinned Go module builds bin/worklease on supported POSIX targets; version text/JSON reports schemaVersion 2 and injected version, commit, buildTime and Go version.
- [ ] #2 Config tests cover flag/env/YAML/default precedence and source tracking, session selection, blank values, durations/bounds, unknown YAML fields/types, symlink rejection and explicit versus default missing configuration.
- [ ] #3 Reason and output tests cover every amended exit family, exactly one JSON envelope, unknown commit state, and no credential echo even on parser, partial-commit or invalid-UTF-8 failures.
- [ ] #4 The CLI skeleton has injected writers, signal cancellation, stable short options and per-command registration; help examples and config env references are tested.
- [ ] #5 Go build/format/vet/staticcheck/test/race/vulnerability tasks and the four-platform CI matrix are added beside Python; mise run ci-go passes and bootstrap introduces no remote implementation.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
