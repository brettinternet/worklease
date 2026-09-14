---
id: TASK-107
title: Implement the opt-in remote claim authority
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:03'
labels:
  - remote-authority
dependencies:
  - TASK-107.12
references:
  - internal/lease
  - internal/store
  - internal/handle
  - internal/mcp
  - internal/cli
  - TASK-85
  - TASK-97
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
priority: high
type: feature
ordinal: 132000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Go release currently coordinates cooperating agents on one host. `docs/remote-claim-authority.md` records the owner decisions of 2026-09-12 and 2026-09-13, and contract section 20 authorizes this planned implementation slice. No remote runtime is implemented yet.

The initial capability is opt-in and self-hosted. One `worklease serve` process serves one namespace through authenticated HTTPS, using the existing Go `lease.Service` and SQLite store with one writer on one host. The CLI and local stdio MCP adapter act as clients. Guarded effects always run on the client host. The server admits only configured delimiter-terminated portable prefixes, persists admitted TTL and hold limits, uses invite-based installation authentication with `read`, `write`, and `admin` roles, binds every authenticated request to an immutable expected restore incarnation, and enters namespace recovery mode after restore. Offline initialization, restore, bootstrap reissue, and retirement hold the hosted lock.

Built-in remote clients durably save exact pending state before every mutation, including enrollment, administration, and `--no-handle` requests. Pending state supports exact retry and recovery but is not a complete history of provider work. Reopening after restore requires an independently retained installation inventory, enumerable pending sets or equivalent independently verified coverage, known outcomes and cessation for retained unresolved work and the known lost tail, and namespace-wide cessation coverage for provider and background effects. A fully missing completed operation may remain an unenumerated history gap only when independent evidence establishes no residual effect. The reopening record includes the selected durable backup cutoff, the interval through old-authority cessation, unknown bounds, and bounded private evidence references.

Repository enrollment and aliases, cross-host transfer, audited recovery import, a durable completed-history client journal, admission backpressure, browser login or a control plane, multi-namespace serving, Postgres, and high availability remain deferred. A restore drill that cannot meet the recovery target or establish the required evidence is the trigger to revisit the journal or recovery import.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every subtask is Done with objective acceptance evidence recorded in its task; no failed scenario may be treated as permission to close a task.
- [ ] #2 One `worklease` binary serves one namespace on one host while two client hosts with distinct checkout roots contend on one portable key through both the CLI and MCP adapter, and every guarded effect runs only on a client host.
- [ ] #3 Restore reopening remains closed unless independent installation inventory and pending-set coverage, outcomes for retained unresolved work and the known lost tail, and namespace-wide provider and executor cessation evidence are complete; a fully missing completed operation is allowed only as an attested history gap backed by independent no-residual-effect evidence.
- [ ] #4 The standard runtime opens no listener and performs no network request before or after this epic unless the user explicitly invokes remote profile management, selects a remote profile, or runs `serve`; local reads remain setup-free and local behavior remains unchanged apart from the shared schema bump.
- [ ] #5 The shipped docs keep one SQLite writer, one namespace per server, portable prefixes, persisted limits, invitation roles, immutable restore binding, and namespace recovery mode, and retain every deferred item and its trigger without claiming high availability.
<!-- AC:END -->
