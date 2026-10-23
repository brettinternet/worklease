---
id: TASK-130.6
title: Expose `queue next` and `--claim` as the MCP `queue_next` tool
status: done
type: feature
priority: high
assignees: ['@pi']
labels: [work-queue, mcp, reviewed]
milestone: m-1
parent: TASK-130
blocked_by: [TASK-130.5]
rank: a563b48c20563b440
refs: [internal/mcp/mcp.go, internal/mcp/lifecycle.go, internal/mcp/renew.go, docs/work-queue-tui-proposal.md]
created: 2026-09-23T16:44:00Z
updated: 2026-09-25T14:53:00Z
---

Some agent loops talk to Worklease only through the MCP stdio server, not the CLI. They need the same select-and-claim step as `queue next --claim` (D28), or they fall back to the slow pick, read, then acquire race. D8 now lets the MCP server import the queue core and adapters, but not the TUI, for this one tool. Existing MCP tools stay unchanged.

## Acceptance criteria

- [x] A new `queue_next` MCP tool accepts `view` and optional `claim` plus the acquire session/TTL/maxHold/autoHeartbeat inputs, and returns the same result schema as `worklease queue next --json` (candidate, readiness, skipped candidates, no-work reasons)
- [x] With `claim: true`, the tool shares the TASK-130.5 selection-and-acquire core. The claim returns an opaque `lease` reference usable with the existing heartbeat, verify, checkpoint, and release tools, and follows the existing MCP acquire session, autoHeartbeat, maxHold, and pending-recovery rules
- [x] Existing MCP tool names, schemas, and behavior are unchanged, as shown by the existing MCP tests passing unmodified
- [x] The MCP server performs no source access or queue configuration load until `queue_next` is called, and does not import TUI packages (checked by an import test)
- [x] A missing or invalid queue configuration yields a structured tool error, not a server failure
- [x] The same N-way concurrency test as TASK-130.5 passes through MCP clients, including mixed MCP and CLI contenders on identical resources
- [x] The `instructions` tool's loop topic and docs/queue.md show the MCP loop

## Definition of done

- [x] `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] Any decision (D1-D28) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit

## Plan

1. Reuse queue next selection/acquire and MCP lease lifecycle without changing existing tools. 2. Add queue_next dispatch, lazy config load, structured errors, docs. 3. Test MCP equivalence, concurrency and imports; run gates, review, integrate.

## Notes

Implementation adf0856 merged locally to main. MCP queue_next reuses CLI selection and fresh identity checks with MCP lease acquisition; runtime profile drift and candidate resource redaction corrected after one review. Tests: TestMCPQueueNextLazyConfigAndLifecycle (schema, lazy invalid config, CLI/MCP key parity and contention, lease lifecycle), TestMCPQueueNextEightMixedContenders (8 concurrent mixed workers), TestMCPQueueNextRejectsRemoteProfileDrift, TestMCPDoesNotImportTUI, unchanged MCP suite. mise run lint, format-check, test, typecheck, hooks all passed. D8/D28 proposal already describes MCP core exception and queue_next; no refinement needed.

Post-completion review (merge 71f5f54): MCP queue_next acquire now re-checks the pinned authority ID inside the acquiring open; invalid queue.yaml is classified config-invalid instead of internal (LoadQueue). Regression tests added; no follow-up needed.

## Summary

Added MCP queue_next read-only and select-and-claim with opaque lease lifecycle, lazy queue loading, authority pin, and exact resource output. Verified mixed contention, profile drift, existing MCP tests, and all project gates; merged adf0856 into main.
