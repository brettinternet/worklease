---
id: TASK-108
title: 'Add shell completion for Bash, Zsh, and Fish'
status: To Do
assignee: []
created_date: '2026-09-14 02:13'
labels:
  - cli
  - ux
dependencies: []
references:
  - internal/cli/root.go
  - internal/cli/commands.go
documentation:
  - README.md
  - docs/cli-reference.md
priority: medium
type: enhancement
ordinal: 146000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Worklease currently rejects the completion-generation flag exposed by its CLI framework and ships no completion command, scripts, or installation guidance. Users must remember a broad nested command and option surface manually. Add static, side-effect-free completion for the common supported shells without reading claim state or exposing credentials.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A documented Worklease command emits shell completion for Bash, Zsh, and Fish, rejects unsupported shells clearly, and does not create or mutate Worklease state.
- [ ] #2 Completion covers visible root and nested commands, command aliases, global options, and command-local options from the registered CLI tree; hidden/internal completion plumbing is not suggested.
- [ ] #3 Generated completion is deterministic and ANSI-free, and automated tests exercise representative root, nested-command, alias, global-option, and local-option completions for each supported shell.
- [ ] #4 README installation guidance shows how to enable completion in Bash, Zsh, and Fish, and the CLI reference documents the completion command.
<!-- AC:END -->
