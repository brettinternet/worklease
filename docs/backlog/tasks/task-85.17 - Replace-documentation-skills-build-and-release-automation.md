---
id: TASK-85.17
title: 'Replace documentation, skills, build, and release automation'
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.15
  - TASK-85.16
references:
  - README.md
  - docs
  - skills/worklease-workflow
  - scripts
  - .github/workflows
  - ../hum/internal/cli/man.go
parent_task_id: TASK-85
priority: high
type: chore
ordinal: 109000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Prepare the Go implementation as the only supported Worklease distribution. Replace Python-generated documentation and PyInstaller packaging with Go-native command documentation and reproducible release archives while updating agent instructions and the workflow skill to match the final interface. Reuse the current archive layout where useful for mise installation, not for compatibility with Python internals.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 README, command reference, claim model, MCP guide, emitted instructions, workflow skill, Backlog-managed workflow guide, changelog, and generated man page describe only the final Go behavior and its safety boundaries.
- [ ] #2 CI runs formatting, vetting, tests, race tests where supported, vulnerability or dependency checks, documentation generation, and artifact smoke tests on Linux and macOS.
- [ ] #3 Tagged builds produce reproducible Linux amd64/arm64 and macOS amd64/arm64 mise-compatible archives containing the Go binary and version-matched manual plus a complete verified checksum manifest.
- [ ] #4 Installation and upgrade guidance calls out the incompatible fresh-authority cutover and gives explicit backup or disposal instructions for Python-era state without attempting automatic migration.
- [ ] #5 Release validation installs every archive, exercises representative CLI and MCP workflows, verifies embedded version/schema metadata, and requires no Python or CGO unless the recorded SQLite decision requires CGO.
<!-- AC:END -->
