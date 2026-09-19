---
id: TASK-125
title: Default MCP acquire session identity to the server process
status: Done
assignee:
  - '@brett'
created_date: '2026-09-19 05:00'
updated_date: '2026-09-19 05:25'
labels: []
dependencies: []
modified_files:
  - internal/mcp/mcp.go
  - internal/mcp/mcp_test.go
  - docs/mcp.md
  - CHANGELOG.md
ordinal: 167000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
When neither acquire.sessionId nor WORKLEASE_SESSION_ID is set, the MCP server falls back to a fresh opID() per acquire (internal/mcp/mcp.go, acquire handler). Two acquires in one unconfigured harness session therefore carry unrelated session identities, so nothing ties the claims to the session that made them. The MCP server's lifetime is roughly the harness session, so a session identity generated once at server start is the natural default and gives unconfigured sessions a stable identity without any harness setup. Repeat acquires would then share one contextual handle slot instead of each getting a fresh one, so the ready-handle replacement rule (docs/claim-model.md 'A ready contextual handle may be replaced only after...') must be checked before changing the fallback.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 With no acquire.sessionId and no WORKLEASE_SESSION_ID, every acquire in one MCP server process reports the same non-empty sessionId, and a restarted server reports a different one
- [x] #2 acquire.sessionId and WORKLEASE_SESSION_ID still take precedence over the generated default, in that order
- [x] #3 Repeat acquires on different resources within one unconfigured MCP server process each succeed and return distinct lease references
- [x] #4 A test covers the process-scoped default and its precedence
- [x] #5 docs/mcp.md states the fallback in one sentence
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Generate and store one default session ID when each MCP Server is constructed, while preserving per-call and configured-session precedence.
2. Add MCP tests for stable process defaults, restart uniqueness, precedence, and distinct leases across resources.
3. Update docs/mcp.md and run focused plus repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented a process-scoped fallback by generating Options.SessionID once in NewServer; explicit acquire.sessionId still overrides configured WORKLEASE_SESSION_ID. Verified with go test ./internal/mcp and full mise lint, format-check, test, and typecheck gates.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
MCP servers now generate one stable fallback session identity at startup, while explicit and environment identities retain precedence. Regression coverage proves same-process reuse, restart uniqueness, distinct lease references, and precedence; all repository quality gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
