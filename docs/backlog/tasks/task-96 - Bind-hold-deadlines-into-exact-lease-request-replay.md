---
id: TASK-96
title: Bind hold deadlines into exact lease request replay
status: To Do
assignee: []
created_date: '2026-09-12 22:44'
updated_date: '2026-09-12 22:45'
labels:
  - go-rewrite
dependencies: []
references:
  - internal/lease/service.go
  - internal/mcp/mcp.go
priority: medium
type: bug
ordinal: 121000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Acquire, heartbeat, and checkpoint accept HoldUntil and use it to cap authority expiry, but omit it from request hashes and durable pending inputs. A reproduced heartbeat replay changes HoldUntil from +2m to +30m with the same operation ID, TTL, and requestNotAfter and is accepted as idempotent instead of operation-request-mismatch. This affects exact intent identity and uncertain recovery, not a global authority hold policy: section 12 explicitly permits direct CLI takeover outside the MCP hold budget. Preserve that exception. MCP acquire also recomputes the ready handle hold deadline after waiting, so recovery before and after persistence must agree on the intended absolute deadline.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Changing the effective hold deadline on an existing acquire, heartbeat, or checkpoint request is rejected as changed intent
- [ ] #2 CLI and MCP persist and replay the exact authority-affecting hold deadline through interrupted dispatch and handle persistence
- [ ] #3 MCP wait and acquire recovery use a consistent fixed hold deadline without extending it on recovery or restart
- [ ] #4 Direct explicit CLI lifecycle takeover remains outside the MCP automatic hold budget, with compatibility for existing pending handles explicitly resolved and tested
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Independent reviewer reproduced changed-HoldUntil heartbeat replay with a temporary Go overlay; primary reviewer reran it and confirmed Idempotent=true with the original receipt. Durable reproduction and the intentional CLI takeover exception are recorded in docs/reviews/go-rewrite-boundaries-follow-up.md.
<!-- SECTION:NOTES:END -->
