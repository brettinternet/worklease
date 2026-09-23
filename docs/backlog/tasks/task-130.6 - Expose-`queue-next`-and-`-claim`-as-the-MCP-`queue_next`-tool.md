---
id: TASK-130.6
title: Expose `queue next` and `--claim` as the MCP `queue_next` tool
status: To Do
assignee: []
created_date: '2026-09-23 16:44'
labels:
  - work-queue
  - mcp
milestone: m-1
dependencies:
  - TASK-130.5
references:
  - internal/mcp/mcp.go
  - internal/mcp/lifecycle.go
  - internal/mcp/renew.go
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-130
priority: high
type: feature
ordinal: 48000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Some agent loops talk to Worklease only through the MCP stdio server, not the CLI. They need the same select-and-claim step as `queue next --claim` (D28), or they fall back to the slow pick, read, then acquire race. D8 now lets the MCP server import the queue core and adapters, but not the TUI, for this one tool. Existing MCP tools stay unchanged.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A new `queue_next` MCP tool accepts `view` and optional `claim` plus the acquire session/TTL/maxHold/autoHeartbeat inputs, and returns the same result schema as `worklease queue next --json` (candidate, readiness, skipped candidates, no-work reasons)
- [ ] #2 With `claim: true`, the tool shares the TASK-130.5 selection-and-acquire core. The claim returns an opaque `lease` reference usable with the existing heartbeat, verify, checkpoint, and release tools, and follows the existing MCP acquire session, autoHeartbeat, maxHold, and pending-recovery rules
- [ ] #3 Existing MCP tool names, schemas, and behavior are unchanged, as shown by the existing MCP tests passing unmodified
- [ ] #4 The MCP server performs no source access or queue configuration load until `queue_next` is called, and does not import TUI packages (checked by an import test)
- [ ] #5 A missing or invalid queue configuration yields a structured tool error, not a server failure
- [ ] #6 The same N-way concurrency test as TASK-130.5 passes through MCP clients, including mixed MCP and CLI contenders on identical resources
- [ ] #7 The `instructions` tool's loop topic and docs/queue.md show the MCP loop
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D28) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
