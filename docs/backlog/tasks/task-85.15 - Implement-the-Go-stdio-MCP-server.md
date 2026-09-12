---
id: TASK-85.15
title: Implement the stdio MCP server
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.10
  - TASK-85.12
  - TASK-85.13
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - ../hum/internal/mcp/server.go
  - ../hum/internal/mcp/server_test.go
  - ../hum/internal/cli/mcp.go
  - src/worklease/mcp_server.py
  - docs/mcp.md
  - tests/test_mcp.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 107000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Coding agents integrate through MCP. The Go binary must serve it from the same process without a second runtime, keep credentials server-side, and behave under cancellation, EOF, and concurrency exactly as contract section 12 specifies, borrowing the proven stdio implementation from hum.

Read first: contract sections 2 (D13), 4 (mcp row), 6.3, 9 (MCP handles), 11 (watch limits), 12, 18. Pattern: `../hum/internal/mcp/server.go` (rpc types, requestRegistry, handlerTracker, responseTransport, the Serve loop with the scanner buffer limit, negotiateProtocolVersion, structuredToolContent) and `../hum/internal/mcp/server_test.go`; `../hum/internal/cli/mcp.go` for wiring. Python evidence: `src/worklease/mcp_server.py` (tool schemas, auto heartbeat loop, max hold, handle files), `docs/mcp.md`, and `tests/test_mcp.py` (all 18 tests, notably test_private_handles_restart_and_reference_validation, test_concurrent_mutation_returns_stable_busy_error, test_automatic_heartbeat_runs_before_half_ttl, test_max_hold_stops_without_release_then_claim_expires, test_stdio_eof_stops_renewal_without_releasing, test_release_stops_heartbeat_and_removes_handle_only_on_success).

Deliver in `internal/mcp`: a `Server` with `Serve(ctx, reader, writer)` implementing `initialize` (version negotiation), `tools/list` with JSON Schemas for the eleven tools of contract 12, `tools/call` dispatch to the same lease, ledger, watch, guard, and resource services the CLI uses, `notifications/cancelled`, duplicate in-flight id rejection, the 4 MiB message limit, 8 concurrent calls, serialized writes, and EOF or parent-cancel shutdown with a 5 s drain; lease references and handle files at `handles/mcp-<ref>.json` through `internal/handle`; a per-lease mutex serializing automatic heartbeat with explicit mutations; automatic heartbeat at ttl/2 with maxHold, stopping on failure without releasing; redaction of tokens and revisions from every result, log line, and error; domain failures as `isError` tool results with the structured error. Deliver `worklease mcp` in `internal/cli`.

Owned paths: `internal/mcp`, `internal/cli/mcp.go` and tests. Out of scope: HTTP transport, resources and prompts capabilities, client configuration files (TASK-85.16).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Real stdio tests (pipes to a Serve goroutine and to a subprocess of the test binary) prove initialize negotiates 2025-06-18, 2025-03-26, and 2024-11-05 and rejects others, tools/list returns exactly the eleven tools with valid JSON Schemas, and unknown methods return -32601.
- [ ] #2 Robustness tests prove duplicate in-flight ids are rejected, a message over 4 MiB terminates the session with a clear error, malformed JSON returns -32700 without stopping the server, the ninth concurrent call waits rather than failing, notifications/cancelled aborts a running watch within 600 ms, and EOF stops the server within 5 s leaving claims and handles intact with no goroutines running.
- [ ] #3 Lifecycle tests prove acquire, status, heartbeat, checkpoint, verify, and release over MCP interoperate with CLI commands on the same home (a CLI `status -r R` sees the MCP claim and a CLI `release --handle <mcp handle path>` is honored), lease references survive a server restart, an invalid reference fails with a stable reason, and results, logs, and errors never contain the token, token hash, or revision (asserted against the known test token).
- [ ] #4 Heartbeat tests with an injected clock prove automatic heartbeat renews before ttl/2, is serialized with an explicit heartbeat (no stale-revision), stops at maxHold without releasing, stops after expiry or ownership loss, stops on release only when release succeeded, and autoHeartbeat is reported as active, stopped, or disabled.
- [ ] #5 The watch and events tools enforce the 60 s timeout cap and return the same shapes as the CLI, and `mise run ci-go` passes including -race for internal/mcp.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
