---
id: TASK-107.4
title: >-
  Extend the lease service with incarnation binding, admission bounds, and
  recovery mode
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies:
  - TASK-107.2
references:
  - internal/lease/service.go
  - internal/lease/helpers.go
  - internal/lease/reconciliation.go
  - internal/resource/resource.go
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 136000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The remote authority reuses `lease.Service`, but several server-enforced invariants belong below the HTTP layer so they hold for every caller inside one serialized transaction rather than in middleware. From the design: check `authorityId` and `expectedRestoreId` before replay lookup or mutation; return `restoreId` and authority time in results; embed `restoreId` in cursors; admit only listed delimiter-terminated portable prefixes and always reject the reserved host-local prefixes; reject over-bound TTL or hold without rewriting hashed intent and persist the admitted maximum TTL and absolute hold deadline so every extension path enforces them; in recovery mode refuse new `BeginOperation` and admit an acquire only when it covers the transitive closure of an unresolved predecessor; resolve retained replay before new-admission checks; record the authenticated installation on every mutation separately from the agent label; and end an epoch with `revoked` on administrative claim revocation.

These are typed-service extensions and must stay storage-neutral: no HTTP or SQLite row types in request or result structs. Local callers continue to work with a local-only admission policy that admits every key, so local behavior is unchanged.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every mutating service method accepts an expected incarnation and fails `authority-restored` with the current `restoreId` when it differs, before any replay lookup or write; results carry `restoreId` and `authorityTime`; cursors embed `restoreId` and an old-incarnation cursor fails with the frozen reason instead of silently restarting.
- [ ] #2 An admission policy (allowed delimiter-terminated prefixes, TTL bound, hold bound) is evaluated inside the acquire transaction: an unlisted prefix or a reserved `path:`, `backlog-md:`, or `markdown:` prefix fails `resource-not-enrolled` before any write even through raw input or a misconfigured allowlist; over-bound TTL or hold is rejected, not clamped; changing bounds leaves existing leases and their exact replay untouched.
- [ ] #3 The admitted maximum TTL and absolute hold deadline are persisted per claim; heartbeat, `BeginOperation`, `RenewOperation`, and same-host transfer cannot extend expiry past them, and a same-host successor inherits the predecessor deadline.
- [ ] #4 With recovery mode set, `BeginOperation` fails for every claim, an acquire that does not cover the full transitive resource set of an unresolved predecessor is refused, replay of a completed operation returns its receipt while a still-started one returns `unknown-outcome`, and clearing recovery mode restores ordinary admission without modifying existing claims.
- [ ] #5 Administrative revocation ends an active epoch with reason `revoked`, leaves started operations unresolved, appends a public event, and leaves the resource immediately acquirable; every mutation records the caller installation identity separately from `agentId`, and replay authentication order is installation, role, authority and incarnation, then epoch credential.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
