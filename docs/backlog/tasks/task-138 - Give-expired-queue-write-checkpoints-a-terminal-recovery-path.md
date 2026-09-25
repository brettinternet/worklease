---
id: TASK-138
title: Give expired queue write checkpoints a terminal recovery path
status: To Do
assignee: []
created_date: '2026-09-25 15:45'
labels:
  - work-queue
milestone: m-1
dependencies: []
references:
  - internal/queue/write.go
  - internal/cli/queue_recovery.go
documentation:
  - docs/work-queue-tui-proposal.md
priority: medium
type: bug
ordinal: 60000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found reviewing TASK-132.1. When a provider write is verified but its Worklease checkpoint did not commit before intent.CheckpointNotAfter (for example a lost checkpoint response followed by a laptop sleep past the default one-hour deadline, or read-back that first succeeds after the deadline), WritePipeline.finishCheckpoint returns 'checkpoint replay deadline expired; reconciliation required' (internal/queue/write.go). But Reconcile only accepts status 'unknown' with no receipt or verified read-back, so it refuses this record. The record stays unresolved forever, is never pruned, and WritePipeline.Start refuses every later queue write on that item ('item has unresolved provider write'). The only exit today is hand-editing the owner-private journal. The fix needs a design decision: an operator-attested 'provider verified, checkpoint missing' outcome (distinct from no-commit reconciliation, and never replaying an expired checkpoint request), or an automatic terminal outcome once the authority proves the checkpoint can no longer commit.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A verified provider write whose checkpoint can no longer commit reaches a terminal journal status that is distinct from verified and from no-commit reconciliation
- [ ] #2 Reaching that status never re-dispatches the provider write and never replays an expired checkpoint request
- [ ] #3 After it, a new queue write on the same item is no longer refused by the unresolved-record guard
- [ ] #4 The CLI 'queue recovery' commands and the TUI Recovery view expose the path, and docs/queue.md describes it
- [ ] #5 A fake-adapter test advances the clock past CheckpointNotAfter with a verified provider effect and no committed checkpoint and exercises the path
<!-- AC:END -->
