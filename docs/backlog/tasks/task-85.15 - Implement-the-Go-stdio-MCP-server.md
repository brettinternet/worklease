---
id: TASK-85.15
title: Implement the Go stdio MCP server
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.12
  - TASK-85.13
  - TASK-85.14
references:
  - ../hum/internal/mcp/server.go
  - ../hum/internal/mcp/tools.go
  - ../hum/internal/cli/mcp.go
  - src/worklease/mcp_server.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 107000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Expose the retained coordination workflow to coding agents from the same Go binary. Borrow the proven dependency-light stdio transport, cancellation, bounded concurrency, and serialized-response patterns from hum rather than introducing a Python runtime or an unnecessarily broad MCP framework. MCP should adapt the same application services as the CLI and keep private lease capabilities server-side.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The worklease binary serves a documented stdio MCP endpoint with typed tools for retained key, acquire, status/list, heartbeat, checkpoint, verify, watch, and release workflows selected by the product contract.
- [ ] #2 The server validates protocol versions and request shapes, bounds message size and concurrent requests, serializes responses, handles cancellation, and shuts down promptly on EOF or parent cancellation without leaking goroutines.
- [ ] #3 Lease credentials and revisions remain in owner-only server-managed handles; structured results, text, logs, and protocol errors never expose them.
- [ ] #4 Automatic heartbeat is serialized with explicit mutations, uses finite maximum hold, stops on expiry/shutdown/cancellation, and never silently releases caller work.
- [ ] #5 Real stdio integration tests cover initialization, tool discovery, concurrent and duplicate IDs, malformed and oversized requests, cancellation, EOF, automatic heartbeat, CLI interoperability within the Go authority, and redaction.
<!-- AC:END -->
