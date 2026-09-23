---
id: TASK-133.2
title: Host external adapters as supervised processes
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-133.1
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-133
priority: medium
type: feature
ordinal: 44000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The queue must run an external adapter as one supervised, long-lived process per source, never one per item (plan section 11). A crash isolates its own source. A crash after dispatching a write leaves the mutation uncertain and routes it into S6 recovery. Installation needs explicit executable and version selection with user approval, and nothing is ever downloaded or run automatically.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 queue.yaml can declare an external adapter by explicit executable path and expected version, and the queue refuses to run it until the user approves it through an explicit command that records approval (for example, by executable digest)
- [ ] #2 The host speaks the TASK-133.1 protocol with request IDs, cancellation, deadlines, size bounds, and backpressure, and it restarts a crashed adapter with bounded backoff
- [ ] #3 A crash, hang, or malformed message affects only that source, which shows a diagnostic. A crash after dispatching a write marks the operation unknown in the S6 journal
- [ ] #4 The adapter process gets a minimized environment and only source-scoped credential references. Worklease bearer credentials never reach it, as tested with canaries
- [ ] #5 stderr is bounded, redacted, and surfaced as diagnostics
- [ ] #6 Tests use a scripted fake adapter binary to cover crash, hang, malformed output, oversized messages, cancellation, and approval refusal
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
