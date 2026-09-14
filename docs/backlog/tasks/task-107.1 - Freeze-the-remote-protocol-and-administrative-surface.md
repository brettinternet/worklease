---
id: TASK-107.1
title: Freeze the remote protocol and administrative surface
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:03'
labels:
  - remote-authority
dependencies: []
references:
  - internal/lease/service.go
  - internal/reason/reason.go
  - internal/watch/watch.go
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: spike
ordinal: 133000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Freeze one authoritative remote protocol specification before client or server implementation. Map the actual typed requests and results in `internal/lease`, plus the required typed administrative extensions, into explicit wire fields. The specification owns the exact routes, flags, protocol version, envelopes, error mapping, cancellation behavior, size limits, replay rules, and serialization order. It must not expose SQLite details or reserve wire names elsewhere before this task settles them.

The specification distinguishes bounded unauthenticated metadata discovery and health from invite-authenticated enrollment and installation-authenticated API calls. Metadata discovery exposes only `authorityId`, `restoreId`, supported protocol version information, and authority time. Enrollment authenticates with the invite and carries `authorityId` plus immutable `expectedRestoreId`. Every other API route, including reads and cursor use, requires an installation bearer and incarnation binding. For mutations, the serialized order is authenticated installation and role, authority and restore incarnation, epoch credential where applicable, retained exact replay, then new-admission checks.

Enumerate which operations are offline-only, client-local, HTTP, CLI-only, or exposed through the existing MCP adapter so the protocol cannot imply remote provider execution, a retirement route, enrollment through MCP, or remote `replace-file`. Assign data and replay ownership to TASK-107.2, domain and admission ownership to TASK-107.4, offline lifecycle ownership to TASK-107.5, and authentication ownership to TASK-107.6. Then freeze the specification by reference through the section 15 amendment procedure and record the required TASK-85 comment.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A Backlog protocol document maps every supported lifecycle, read, watch, authentication, and administrative request and result field onto real `internal/lease` types or named typed extensions, and identifies excluded local compatibility fields including `LegacyRequestHash`.
- [ ] #2 The document fixes exact routes and flags, protocol and version handling, request and response size bounds, cancellation behavior, non-cacheable responses, the 30-second watch long poll, cursor incarnation binding, and every error-to-HTTP mapping including unsupported version, authentication, authorization, cancellation, conflict, recovery, and storage failures.
- [ ] #3 The unauthenticated surface is limited to health and bounded identity-only metadata discovery. Enrollment uses invite authentication with `authorityId` and immutable `expectedRestoreId`; every other route, including reads and cursors, requires a valid installation bearer and incarnation check.
- [ ] #4 The document fixes mutation ordering inside the serialized boundary as installation and role, authority and incarnation, epoch credential where applicable, retained exact replay, then new-admission checks, with revoked and absent credentials taking precedence as specified.
- [ ] #5 A matrix identifies offline-only, client-local, HTTP, CLI-only, and existing MCP operations. It excludes a retirement HTTP route, server-side effect execution, remote `replace-file`, MCP enrollment, recovery import, and cross-host transfer.
- [ ] #6 The specification assigns schema and replay data to TASK-107.2, domain invariants to TASK-107.4, offline lifecycle composition to TASK-107.5, and authentication primitives to TASK-107.6 without exposing SQLite structures on the wire.
- [ ] #7 Contract section 20 is amended through section 15 to freeze this document by reference, section 17 records the amendment, and TASK-85 receives the matching summary comment.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
