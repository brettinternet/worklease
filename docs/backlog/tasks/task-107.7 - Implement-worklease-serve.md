---
id: TASK-107.7
title: Implement worklease serve
status: Done
assignee:
  - '@pi'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 08:03'
labels:
  - remote-authority
dependencies:
  - TASK-107.3
  - TASK-107.6
references:
  - internal/watch/watch.go
  - internal/lease/service.go
  - cmd/worklease
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
modified_files:
  - internal/cli/commands.go
  - internal/gc/gc.go
  - internal/lease/remote_authz.go
  - internal/lease/service.go
  - internal/ledger/ledger.go
  - internal/reason/reason.go
  - internal/remoteadmin/gc.go
  - internal/server/admin.go
  - internal/server/handlers.go
  - internal/server/reconcile.go
  - internal/server/routes.go
  - internal/server/server.go
  - internal/server/server_test.go
  - internal/watch/watch.go
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 139000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement `worklease serve` as the transport over the frozen typed service and authentication contract. It serves one namespace, holds the hosted lock for its lifetime, applies rate limits plus bounded JSON request and response handling, rejects unknown fields and unsupported versions, maps every typed error, validates response envelopes, uses non-cacheable identity-bearing responses, and implements the 30-second watch long poll without holding a storage transaction. Health and bounded identity-only metadata discovery are public. Enrollment uses invite authentication. Every other route, including reads and cursors, requires an installation bearer.

The server configuration supplies the listen address, TLS material or an explicit development-only plaintext flag, admitted portable prefixes, and TTL/hold bounds. Configuration changes take effect after an explicit stop and start. The HTTP layer performs early structural checks, while authenticated incarnation, authorization, replay, recovery, and policy checks remain in the serialized service transaction. Resource keys may appear only in authorized structured requests, results, and errors where the contract requires them. They never appear in URLs or access logs. Private fields appear only in authorized private projections, and bearer secrets never appear in output. Use the standard library for TLS and rate limiting unless contract D2 is amended before adding a runtime dependency.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `worklease serve --server-config FILE` recognizes the hosted home and takes its lock before opening or migrating the database, serves one namespace, drains in-flight requests for a bounded shutdown deadline on SIGTERM, then cancels pending requests and long polls and releases the lock after shutdown.
- [x] #2 Health and bounded identity-only metadata discovery are public and disclose only authority identity, restore identity, supported protocol information, authority time, and health. Health, metadata, and enrollment have explicit rate and body limits. Enrollment requires invite authentication; all other routes require an installation bearer.
- [x] #3 Handlers reject unknown fields, unsupported versions, oversized bodies or responses, invalid structure, and cancellation according to every frozen error mapping. Every application response is validated, carries fresh `authorityId`, `restoreId`, and `authorityTime`, and uses `Cache-Control: no-store`.
- [x] #4 Early transport validation does not replace transaction checks. Authenticated mutations recheck bearer state, role, authority, incarnation, epoch credential, replay, recovery state, and admission policy through the service in the frozen order.
- [x] #5 The server opens a marked hosted home only through the remote service configuration, so no permissive local admission path can serve or mutate it. Hosted migrations occur through this approved locked entry point.
- [x] #6 The watch route returns a coherent snapshot and incarnation-bound cursor, waits at most 30 seconds without holding a storage transaction, and reports retention gaps explicitly. An old-incarnation cursor fails `authority-restored`, and `--wait` remains a client-side loop.
- [x] #7 TLS is required outside the explicit development flag. Configuration updates require stop and start; changed prefixes and bounds affect new admission while persisted limits, existing lifecycle, recovery, transfer, and replay remain valid.
- [x] #8 The transport has no SQLite row dependency and exposes no server-side provider or child-process execution. Resource keys occur only in authorized structured content, private fields only in authorized private projections, and resource keys and bearers never appear in URLs or access logs. Bearer secrets never appear in any response or error. TLS and rate limiting add no runtime dependency outside contract D2 without an amendment.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a strict, bounded HTTP transport package with deployment-owned server configuration, protocol envelopes, authentication-header extraction, error/status mapping, rate limits, safe logging, and graceful shutdown.
2. Bind every frozen V1 route to the existing lease, ledger, watch, and GC services through typed request adapters while preserving domain-side authorization and replay checks.
3. Register worklease serve --server-config FILE [--dev-http], opening only a ready marked hosted home under its process-lifetime lock before listening.
4. Add transport and CLI tests for config/TLS safety, envelopes and bounds, authentication, lifecycle/admin routing, watch cancellation, redaction, lock lifetime, and shutdown.
5. Run focused tests, independent review, repository quality gates, then finalize and commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the frozen worklease-http/1 server transport, strict deployment configuration, hosted lock lifecycle, TLS/dev HTTP safety, bounded JSON and response projection, public endpoint rate limiting, authentication headers, route bindings, durable admin GC replay, transaction-coherent authenticated reads, graceful shutdown, cancellable 30-second watch polling, and redacted access logs. Added focused integration tests covering metadata, enrollment, acquire, distinct installation/claim bearers, heartbeat, strict fields, watch cancellation, GC replay mismatch, lock lifetime, and shutdown. Focused race tests and lint/format/test/typecheck gates pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented worklease serve and the frozen worklease-http/1 transport in commit 6d5f529. The server holds the hosted writer lock for its lifetime, enforces TLS or loopback-only development HTTP, strictly bounds and validates requests/responses, authenticates every protected route, preserves domain transaction ordering and replay, rate-limits public discovery/enrollment, projects non-cacheable redacted envelopes, and provides cancellable 30-second watch polling plus bounded graceful shutdown. Verified by end-to-end server tests for enrollment, distinct bearer scopes, lifecycle mutation, cancellation, replay, lock lifetime, and shutdown; focused race tests; transport dependency inspection; hooks; and mise run ci on commit 6d5f529.
<!-- SECTION:FINAL_SUMMARY:END -->
