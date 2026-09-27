---
id: TASK-151
title: Let a repository propose queue sources for the user to adopt
status: To Do
assignee: []
created_date: '2026-09-27 05:56'
labels:
  - work-queue
dependencies: []
references:
  - internal/config/queue.go
  - docs/work-queue-tui-proposal.md
  - docs/queue.md
priority: low
type: feature
ordinal: 91000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Why

Queue configuration lives only in the owner-private `$XDG_CONFIG_HOME/worklease/queue.yaml` (D10). `config.QueuePath` deliberately ignores the checkout, the working directory, and Worklease environment variables, because that file controls launch argv, external adapter executables, credential helpers, checkout paths, and Git network consent. A repository that could supply any of those would get code execution just by being opened.

That leaves no way for a project to share its work sources with collaborators or agents: each person must discover and hand-write them, or run `worklease queue init --checkout PATH` per repository. The proposal already defers a narrow alternative (docs/work-queue-tui-proposal.md, "Project-suggested sources are deferred"): a repository file may only propose sources, which the user reviews and copies into their own queue.yaml. It never proposes adapters, executables, credentials, or launch actions.

Open questions for the implementer to settle: the file name and location in the checkout, how proposals are surfaced (`queue init`, a TUI notice, or both), and how a changed proposal is shown after adoption.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A repository file can propose queue sources and optional views; Worklease never loads it as configuration and never reads it outside an explicit user action.
- [ ] #2 The proposal schema accepts only built-in adapters and source fields that cannot execute code or select credentials; adapters, executables, credential helpers, launch actions, and Git network consent are rejected with an actionable error.
- [ ] #3 Adopting a proposal shows the exact YAML to be added and writes it to the owner-private queue.yaml only after explicit confirmation; --dry-run previews without writing.
- [ ] #4 Adopted sources are ordinary user configuration: later edits to the repository file do not change them until the user adopts again.
- [ ] #5 docs/queue.md and docs/work-queue-tui-proposal.md describe the file, its trust boundary, and the adoption flow.
<!-- AC:END -->
