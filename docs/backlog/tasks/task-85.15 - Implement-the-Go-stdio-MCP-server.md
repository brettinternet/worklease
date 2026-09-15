---
id: TASK-85.15
title: Implement the stdio MCP server
status: Done
assignee:
  - '@pi-01a096ee'
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 19:54'
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
modified_files:
  - internal/cli/commands.go
  - internal/lease/service.go
  - internal/mcp/server.go
  - internal/mcp/mcp.go
  - internal/mcp/lifecycle.go
  - internal/mcp/renew.go
  - internal/mcp/mcp_test.go
  - internal/mcp/acceptance_regression_test.go
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
- [x] #1 Real stdio/subprocess tests prove modern 2026-07-28 per-request versioning/server-discover and specified unsupported-version errors, legacy 2025-11-25 initialize/initialized negotiation, exact eleven-tool schemas, unknown methods, duplicate IDs, malformed/oversized input and serialized responses.
- [x] #2 Eight long-running calls plus queued calls still permit cancellation/EOF processing; shutdown is bounded and leaves handles/claims recoverable with no goroutine leaks.
- [x] #3 Lifecycle tests cover CLI/MCP interoperability, two servers using one reference, pending acquire/heartbeat/release recovery, restart reference use and no bearer/hash leakage; public revision fields are allowed.
- [x] #4 Heartbeat tests prove persisted holdUntil, expiry clamping, no restart extension/resumption, one automatic-renewal owner, serialized explicit mutations and no spurious verify failures from renewal races. A TTL longer than maxHold is capped on initial grant and explicit renewal after restart.
- [x] #5 Events/watch enforce timeout and cursor semantics, checkpoint input rejects embedded credentials, and actual returned lease references can be passed to CLI verify/native hook selection; mise run ci-go passes.
- [x] #6 An end-to-end client uses discovery/schema information, minimal acquire input and only the returned lease for status/verify, checkpoint and release, plus bounded watch/events, without shell calls or token/revision management. Contention and applicable unknown-outcome failures retain structured domain details/commit state, and separate leases do not adopt another loop's claim.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add the Go stdio MCP transport, request registry, version negotiation/discovery, bounded concurrency, cancellation, and exact eleven-tool schemas.
2. Implement private reference-backed lease lifecycle state with cross-process locking, pending-operation recovery, bounded automatic renewal, and CLI/native-hook interoperability.
3. Wire all tools to existing typed services and add real stdio/subprocess, lifecycle, heartbeat, event/watch, redaction, contention, and end-to-end tests.
4. Run focused Go tests and the full repository quality gates, review the diff, then record acceptance evidence and completion.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented commit c497cb0 in an isolated worktree. Added the Go stdio JSON-RPC/MCP server, eleven typed tools, private reference-backed lifecycle handles, cross-process mutation locking and pending replay, bounded renewal/hold enforcement, CLI wiring, and acceptance tests. Independent review found queueing, duplicate-ID, renewal recovery, hold ceiling, schema validation, verify locking, and input-validation defects; all valid findings were fixed. Validation passed: mise run ci-go; mise run lint; mise run format-check; mise run test (339 Python tests); mise run typecheck; mise run hooks; go test -race ./internal/mcp.

Merged implementation to main in c4b8573 and the post-merge test stabilization in 31b2e7c. Post-merge mise run ci-go passed on main.

Correction: the post-merge test stabilization merge commit is 812d9d6 (implementation fix bc0c6d6); 31b2e7c was recorded in error.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented the Go stdio MCP server and all eleven contract tools on the typed lease services with private handle references, bounded request concurrency, cancellation/EOF shutdown, exact pending recovery, cross-process serialization, persisted hold ceilings, automatic renewal ownership, redaction, and CLI interoperability. AC1: TestDuplicateIDsLegacyNegotiationAndMalformedInput, TestOversizedInputAndExactElevenToolSchemas, TestStdioNegotiationSurfaceAndProtocolErrors, and TestSubprocessStdioLifecycle prove protocol/version/schema/input behavior. AC2: TestQueuedToolCallWaitsBehindEightAndEOFIsBounded and TestServeCancellationClosesIdleInputAndBrokenOutput prove queued admission, cancellation, serialized output, and bounded shutdown. AC3: TestReferencesCrossServerPendingRecoveryAndRestartHold and TestCallLifecycleAndRedaction prove cross-server references, CLI verify interoperability, pending heartbeat/release recovery, and redaction; TestMCPArgumentTypesHoldCeilingAndCanonicalInstructions covers pending acquire replay. AC4: TestAutomaticHeartbeatHasOneOwnerAndDoesNotResumeAfterRestart, TestReferencesCrossServerPendingRecoveryAndRestartHold, and the race-enabled MCP suite prove renewal ownership, restart behavior, recovery, and durable expiry ceilings. AC5: TestRejectedCheckpointDoesNotStrandLease, TestEndToEndDiscoveredClientUsesOnlyLeaseReference, and the watch/events lifecycle paths prove credential rejection, cursor/timeouts, and returned-handle CLI verification. AC6: TestEndToEndDiscoveredClientUsesOnlyLeaseReference exercises discovery-driven acquire/status/verify/checkpoint/events/watch/release without shell credential or revision management. Final validation: mise run ci-go passed, including gofmt, go test ./..., go test -race ./..., vet/staticcheck, govulncheck, and CGO-disabled build; repository lint, format-check, test, typecheck, and hooks also passed.
<!-- SECTION:FINAL_SUMMARY:END -->
