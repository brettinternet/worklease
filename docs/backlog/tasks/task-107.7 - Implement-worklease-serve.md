---
id: TASK-107.7
title: Implement worklease serve
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:43'
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
- [ ] #1 `worklease serve --server-config FILE` recognizes the hosted home and takes its lock before opening or migrating the database, serves one namespace, drains in-flight requests for a bounded shutdown deadline on SIGTERM, then cancels pending requests and long polls and releases the lock after shutdown.
- [ ] #2 Health and bounded identity-only metadata discovery are public and disclose only authority identity, restore identity, supported protocol information, authority time, and health. Health, metadata, and enrollment have explicit rate and body limits. Enrollment requires invite authentication; all other routes require an installation bearer.
- [ ] #3 Handlers reject unknown fields, unsupported versions, oversized bodies or responses, invalid structure, and cancellation according to every frozen error mapping. Every application response is validated, carries fresh `authorityId`, `restoreId`, and `authorityTime`, and uses `Cache-Control: no-store`.
- [ ] #4 Early transport validation does not replace transaction checks. Authenticated mutations recheck bearer state, role, authority, incarnation, epoch credential, replay, recovery state, and admission policy through the service in the frozen order.
- [ ] #5 The server opens a marked hosted home only through the remote service configuration, so no permissive local admission path can serve or mutate it. Hosted migrations occur through this approved locked entry point.
- [ ] #6 The watch route returns a coherent snapshot and incarnation-bound cursor, waits at most 30 seconds without holding a storage transaction, and reports retention gaps explicitly. An old-incarnation cursor fails `authority-restored`, and `--wait` remains a client-side loop.
- [ ] #7 TLS is required outside the explicit development flag. Configuration updates require stop and start; changed prefixes and bounds affect new admission while persisted limits, existing lifecycle, recovery, transfer, and replay remain valid.
- [ ] #8 The transport has no SQLite row dependency and exposes no server-side provider or child-process execution. Resource keys occur only in authorized structured content, private fields only in authorized private projections, and resource keys and bearers never appear in URLs or access logs. Bearer secrets never appear in any response or error. TLS and rate limiting add no runtime dependency outside contract D2 without an amendment.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
