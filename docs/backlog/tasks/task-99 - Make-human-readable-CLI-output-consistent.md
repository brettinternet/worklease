---
id: TASK-99
title: Make human-readable CLI output consistent
status: To Do
assignee: []
created_date: '2026-09-13 02:41'
labels:
  - cli
  - ux
dependencies: []
references:
  - internal/cli/text.go
  - internal/cli/lease_commands.go
  - internal/cli/ledger_commands.go
  - internal/cli/resource_commands.go
  - internal/cli/watch_commands.go
  - internal/cli/gc_commands.go
  - internal/output/output.go
  - docs/cli-reference.md
priority: medium
type: enhancement
ordinal: 124000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
An audit after compacting `worklease list` found that several text commands still echo their command name as a standalone first line, some documented `--full` flags do not change output, and related views use inconsistent labels, timestamp styles, and state coloring. Human-readable output should be concise and conventional while JSON retains stable operation metadata and complete machine-oriented fields.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Text output for `status`, `history`, `events`, `gc`, `key`, `policy describe`, and `watch` does not echo the command name as a standalone banner; useful outcome sentences and table headers remain.
- [ ] #2 `status --full`, `events --full`, and `policy describe --full` each have a documented, test-covered distinction from their default text view, or the unsupported flag is removed from that command.
- [ ] #3 Human-readable field labels follow one documented vocabulary and casing convention; JSON field names and envelopes remain unchanged.
- [ ] #4 Default `status`, `history`, and `events` views use concise relative timing where timing aids scanning, while full views retain absolute RFC3339 timestamps.
- [ ] #5 History and event state or kind coloring uses the shared TTY-aware semantic palette without coloring identifiers or timestamps; redirected output, `NO_COLOR`, `TERM=dumb`, and JSON contain no ANSI sequences.
- [ ] #6 Affected commands use command-specific text renderers rather than the generic operation-banner writer, and tests cover deterministic ordering, redaction, control-character safety, and the absence of Go map or struct dumps.
- [ ] #7 CLI help and `docs/cli-reference.md` describe compact versus full output and color behavior for the affected commands.
<!-- AC:END -->
