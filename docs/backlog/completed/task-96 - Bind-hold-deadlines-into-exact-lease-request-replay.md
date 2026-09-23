---
id: TASK-96
title: Bind hold deadlines into exact lease request replay
status: Done
assignee:
  - '@pi-01a0980c'
created_date: '2026-09-12 22:44'
updated_date: '2026-09-13 00:16'
labels:
  - go-rewrite
dependencies: []
references:
  - internal/lease/service.go
  - internal/mcp/mcp.go
modified_files:
  - CHANGELOG.md
  - internal/cli/lease_commands.go
  - internal/cli/resource_commands_test.go
  - internal/lease/helpers.go
  - internal/lease/service.go
  - internal/lease/service_test.go
  - internal/mcp/lifecycle.go
  - internal/mcp/mcp.go
  - internal/mcp/mcp_test.go
  - internal/mcp/renew.go
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
- [x] #1 Changing the effective hold deadline on an existing acquire, heartbeat, or checkpoint request is rejected as changed intent
- [x] #2 CLI and MCP persist and replay the exact authority-affecting hold deadline through interrupted dispatch and handle persistence
- [x] #3 MCP wait and acquire recovery use a consistent fixed hold deadline without extending it on recovery or restart
- [x] #4 Direct explicit CLI lifecycle takeover remains outside the MCP automatic hold budget, with compatibility for existing pending handles explicitly resolved and tested
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Bind nonzero authority hold deadlines into acquire, heartbeat, and checkpoint request hashes while preserving the zero-hold direct CLI contract.
2. Persist fixed absolute hold deadlines in MCP pending inputs and replay them unchanged across wait, restart, and interrupted dispatch; migrate legacy pending MCP handles by accepting only their recorded legacy hash on replay.
3. Add service, CLI, and MCP regression tests for changed-intent rejection, pending-handle compatibility, fixed acquire deadlines, and explicit CLI takeover.
4. Run focused tests, independent review, and all repository quality gates before commit and integration.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Independent reviewer reproduced changed-HoldUntil heartbeat replay with a temporary Go overlay; primary reviewer reran it and confirmed Idempotent=true with the original receipt. Durable reproduction and the intentional CLI takeover exception are recorded in docs/reviews/go-rewrite-boundaries-follow-up.md.

Implemented hold-bound lifecycle request hashing and fixed MCP absolute hold persistence/recovery. Added explicit legacy-hash replay only for pre-binding pending handles; fresh CLI takeover clears the MCP hold before persisting its pending intent.
Verification: go test -race -count=1 ./internal/lease ./internal/cli ./internal/mcp; mise run lint; mise run format-check; mise run test; mise run typecheck; mise run hooks. Independent review found one interrupted CLI takeover defect, fixed and re-reviewed with no remaining findings. Implementation commit 5adbff2; merged to main as 5724568.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Bound authority-affecting hold deadlines into exact acquire, heartbeat, and checkpoint intent hashes. MCP now persists one absolute hold across waits and recovery, legacy pending handles replay through an explicit compatibility path, and direct CLI takeover remains unbounded. Verified with focused race tests, all repository gates and hooks, post-merge tests, and independent review.
<!-- SECTION:FINAL_SUMMARY:END -->
