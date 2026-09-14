---
id: TASK-107.7
title: Implement worklease serve
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies:
  - TASK-107.1
  - TASK-107.5
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
The server is the same binary in a second mode: HTTP handlers over the typed lease service and the authentication layer, following the frozen protocol. It owns the transport concerns the service does not: bounded JSON bodies and responses, unknown-field and version rejection, non-cacheable responses, bearer extraction, rate and body limits on the unauthenticated `enroll` and health routes, the 30 s watch long poll with the local adaptive poll inside the server and no storage transaction held while waiting, TLS with plaintext allowed only under an explicit development flag, and a deployment-owned configuration file carrying listen address, TLS material, admitted prefixes, and TTL/hold bounds. It holds the hosted single-writer lock for its lifetime and serves exactly one namespace.

Transport and authentication code must not depend on SQLite driver or row types so a later backend can slot in below the service boundary. Contract D2 restricts runtime dependencies: implement rate limiting and TLS with the standard library or record a D2 amendment before adding a module.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease serve --config FILE` takes the hosted lock, opens the database, and serves the frozen protocol; a second `serve` on the same home fails with the lock reason; SIGTERM drains in-flight requests, releases the lock, and exits cleanly.
- [ ] #2 Every authenticated route rejects unknown fields, unsupported protocol versions, oversized bodies, and missing or mismatched `authorityId` or `expectedRestoreId` with the frozen reasons before calling the service; every application response carries `restoreId`, `authorityTime`, and `Cache-Control: no-store`; no resource key or bearer appears in URLs, access logs, or error bodies.
- [ ] #3 Only health and `enroll` are unauthenticated, both rate-limited and body-bounded; every other route requires a valid installation bearer and enforces the role table.
- [ ] #4 The watch route is a long poll bounded at 30 s that returns a coherent snapshot plus cursor, holds no storage transaction while waiting, and reports cursor gaps explicitly; `--wait` remains a client-side loop.
- [ ] #5 The configuration file supplies listen address, TLS certificate and key or an explicit development plaintext flag, admitted delimiter-terminated prefixes, and TTL/hold bounds; reloading changed prefixes or bounds refuses only new admissions while lifecycle, recovery, same-host transfer, and exact replay continue.
- [ ] #6 The serve package imports no SQLite driver or `internal/store` row types, uses no dependency outside contract D2 without an amendment, and `mise run ci` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
