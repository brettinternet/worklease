---
id: TASK-146
title: Launch the queue TUI from bare `worklease` in an interactive terminal
status: To Do
assignee: []
created_date: '2026-09-26 06:09'
labels: []
dependencies:
  - TASK-145
references:
  - internal/cli/commands.go
  - internal/cli/queue_command.go
  - cmd/worklease-doc-test
  - cmd/worklease-man
priority: medium
type: feature
ordinal: 80000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Bare `worklease` currently prints help and exits 0. Once the queue TUI has a Claims tab (TASK-145), a person typing `worklease` at a terminal is better served by the live TUI than by help text, and users with no task provider still get a useful view of claims.

Agents, scripts, and CI are primary callers of this CLI and rely on bare invocation being non-interactive, so the TUI must launch only for a human at a terminal.

The starting tab is chosen from configuration, not from provider reachability. Falling back to Claims when a configured provider is down would hide the outage and look like missing config; the queue tab already shows degraded sources, and Claims is one keystroke away.

Bare invocation is also the current onboarding path: root help says to start with `worklease acquire --path README.md`. That guidance has to survive in the TUI empty state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Bare `worklease` with no arguments launches the queue TUI only when both stdin and stdout are terminals
- [ ] #2 Bare `worklease` with a non-terminal stdin or stdout prints the current root help and exits 0; `worklease help` and `-h` print help in every case
- [ ] #3 With a valid `queue.yaml`, the TUI opens on the first configured queue view even when sources fail, showing the existing source-error state
- [ ] #4 Without `queue.yaml`, the TUI opens on the Claims tab with a hint to run `worklease queue init`
- [ ] #5 A malformed `queue.yaml` fails with the same error as `worklease queue`
- [ ] #6 Launching the TUI from bare `worklease` creates no authority, store, or config state on a fresh machine
- [ ] #7 The empty Claims tab shows onboarding hints including `worklease acquire --path README.md`, `?` for help, and `q` to quit
- [ ] #8 Root help text, the man page, and doc tests describe the interactive and non-interactive behavior of bare `worklease`
- [ ] #9 Tests cover terminal detection, starting-tab selection for each config state, and no state creation at the lowest layer that reproduces each
<!-- AC:END -->
