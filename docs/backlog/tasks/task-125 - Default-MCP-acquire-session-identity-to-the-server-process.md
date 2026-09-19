---
id: TASK-125
title: Default MCP acquire session identity to the server process
status: To Do
assignee: []
created_date: '2026-09-19 05:00'
labels: []
dependencies: []
ordinal: 167000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
When neither acquire.sessionId nor WORKLEASE_SESSION_ID is set, the MCP server falls back to a fresh opID() per acquire (internal/mcp/mcp.go, acquire handler). Two acquires in one unconfigured harness session therefore carry unrelated session identities, so nothing ties the claims to the session that made them. The MCP server's lifetime is roughly the harness session, so a session identity generated once at server start is the natural default and gives unconfigured sessions a stable identity without any harness setup. Repeat acquires would then share one contextual handle slot instead of each getting a fresh one, so the ready-handle replacement rule (docs/claim-model.md 'A ready contextual handle may be replaced only after...') must be checked before changing the fallback.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 With no acquire.sessionId and no WORKLEASE_SESSION_ID, every acquire in one MCP server process reports the same non-empty sessionId, and a restarted server reports a different one
- [ ] #2 acquire.sessionId and WORKLEASE_SESSION_ID still take precedence over the generated default, in that order
- [ ] #3 Repeat acquires on different resources within one unconfigured MCP server process each succeed and return distinct lease references
- [ ] #4 A test covers the process-scoped default and its precedence
- [ ] #5 docs/mcp.md states the fallback in one sentence
<!-- AC:END -->
