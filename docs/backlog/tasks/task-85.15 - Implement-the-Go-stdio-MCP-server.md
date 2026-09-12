---
id: TASK-85.15
title: Implement the stdio MCP server
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 06:27'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.14
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
Implement the eleven stdio MCP tools from contract section 12 on the same typed services and private handles as CLI. Own internal/mcp and the mcp CLI command. Borrow hum transport code only where it satisfies bounded input, request registry, cancellation and output behavior.

Use cross-process handle locking, exact pending-request recovery and persisted absolute holdUntil. Restart preserves lease references but does not resume automatic renewals or reset maxHold. Tokens stay private; revisions are ordinary non-secret diagnostics. Keep stdin dispatch able to process cancellation/EOF even when all eight tool slots are occupied. Expose lease identity/handlePath so a caller can explicitly bind native hooks; do not imply MCP grants automatically select a contextual handle.

Evidence and patterns (the amended contract is normative): hum `internal/mcp/server.go` (rpc types, requestRegistry, handlerTracker, responseTransport, the Serve loop with the scanner buffer limit) and `internal/mcp/server_test.go`, plus `internal/cli/mcp.go` for wiring; note that hum implements only the legacy initialize era, so the modern 2026-07-28 per-request versioning and `server/discover` are new work verified against the spec links in contract section 12. Python evidence: `src/worklease/mcp_server.py` (tool schemas, auto heartbeat loop, max hold, handle files), `docs/mcp.md`, and all 18 tests in `tests/test_mcp.py`, notably test_private_handles_restart_and_reference_validation, test_concurrent_mutation_returns_stable_busy_error, test_automatic_heartbeat_runs_before_half_ttl, test_max_hold_stops_without_release_then_claim_expires, test_stdio_eof_stops_renewal_without_releasing, test_release_stops_heartbeat_and_removes_handle_only_on_success.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Real stdio/subprocess tests prove modern 2026-07-28 per-request versioning/server-discover and specified unsupported-version errors, legacy 2025-11-25 initialize/initialized negotiation, exact eleven-tool schemas, unknown methods, duplicate IDs, malformed/oversized input and serialized responses.
- [ ] #2 Eight long-running calls plus queued calls still permit cancellation/EOF processing; shutdown is bounded and leaves handles/claims recoverable with no goroutine leaks.
- [ ] #3 Lifecycle tests cover CLI/MCP interoperability, two servers using one reference, pending acquire/heartbeat/release recovery, restart reference use and no bearer/hash leakage; public revision fields are allowed.
- [ ] #4 Heartbeat tests prove persisted holdUntil, expiry clamping, no restart extension/resumption, one automatic-renewal owner, serialized explicit mutations and no spurious verify failures from renewal races. A TTL longer than maxHold is capped on initial grant and explicit renewal after restart.
- [ ] #5 Events/watch enforce timeout and cursor semantics, checkpoint input rejects embedded credentials, and actual returned lease references can be passed to CLI verify/native hook selection; mise run ci-go passes.
- [ ] #6 An end-to-end client uses discovery/schema information, minimal acquire input and only the returned lease for status/verify, checkpoint and release, plus bounded watch/events, without shell calls or token/revision management. Contention and applicable unknown-outcome failures retain structured domain details/commit state, and separate leases do not adopt another loop's claim.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
