---
id: TASK-9
title: Correct lifecycle and guarantee documentation
status: To Do
assignee: []
created_date: '2026-07-14 02:33'
labels:
  - documentation
  - cli
  - workflow
dependencies: []
references:
  - README.md
  - skills/worklease-workflow/SKILL.md
priority: high
type: docs
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the README a runnable and accurate guide to the released Worklease lifecycle and guarantees. Cover existing behavior without implementing TASK-4 checkpoint metadata, TASK-6 unknown-operation reconciliation, or TASK-7 verbose diagnostics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 README provides a complete CLI lifecycle from resource derivation and acquisition through guarded work, authoritative provider checkpoint verification, and release, with every referenced claim value defined.
- [ ] #2 Documentation states that heartbeat and completed exec/replace-file operations advance the active revision, while release validates and consumes the latest revision without advancing it.
- [ ] #3 Documentation distinguishes Worklease lease/operation receipts from caller- or provider-owned durable checkpoints and does not imply that the current CLI has a checkpoint command.
- [ ] #4 Documentation explains coordination-only claims, ownership-loss behavior, unknown outcomes, state-home selection, exact resource identity, token handling, and the boundary between local coordination and provider fencing.
- [ ] #5 README includes concise recipes for singleton commands, scarce local resources, local-agent item ownership, source-wide Markdown replacement, idempotent replay, and locally coordinated remote-provider writes; focused checks validate commands, links, and terminology.
<!-- AC:END -->
