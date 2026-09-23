---
id: TASK-107
title: Implement the opt-in remote claim authority
status: Done
assignee:
  - '@brett'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-15 05:15'
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
- [x] #1 Every subtask is Done with objective acceptance evidence recorded in its task; no failed scenario may be treated as permission to close a task.
- [x] #2 One `worklease` binary serves one namespace on one host while two client hosts with distinct checkout roots contend on one portable key through both the CLI and MCP adapter, and every guarded effect runs only on a client host.
- [x] #3 Restore reopening remains closed unless independent installation inventory and pending-set coverage, outcomes for retained unresolved work and the known lost tail, and namespace-wide provider and executor cessation evidence are complete; a fully missing completed operation is allowed only as an attested history gap backed by independent no-residual-effect evidence.
- [x] #4 The standard runtime opens no listener and performs no network request before or after this epic unless the user explicitly invokes remote profile management, selects a remote profile, or runs `serve`; local reads remain setup-free and local behavior remains unchanged apart from the shared schema bump.
- [x] #5 The shipped docs keep one SQLite writer, one namespace per server, portable prefixes, persisted limits, invitation roles, immutable restore binding, and namespace recovery mode, and retain every deferred item and its trigger without claiming high availability.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Verify all 13 subtasks are Done and their recorded evidence satisfies the parent acceptance criteria.
2. Re-run repository quality gates from an isolated worktree at main.
3. Record parent acceptance evidence, complete TASK-107, commit the authoritative task update, merge it to main, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Parent verification on 2026-09-15: all TASK-107.1 through TASK-107.13 are Done with every acceptance criterion checked and objective verification/final summaries recorded. TASK-107.11 records passing local and repository-managed Lima VM scenario-group 1-5 reports, including two distinct client roots, CLI/MCP contention, client-only effects, and restore/reopening evidence boundaries. TASK-107.12 records the aligned experimental operator/product documentation and no-network opt-in boundary. TASK-107.13 records passing native amd64/arm64 image validation and lifecycle persistence coverage. From isolated worktree task-107-finalize at ea66d2b, mise run lint, mise run format-check, mise run test, and mise run typecheck all passed.

Authoritative task state was reread after completion: status Done and all five parent acceptance criteria checked.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed the opt-in experimental remote claim authority epic. All 13 subtasks are Done with checked acceptance evidence; the two-client TLS CLI/MCP harness and Lima VM run cover contention, client-local effects, fault recovery, and restore reopening constraints; docs preserve the single-writer, explicit opt-in, recovery, and deferred-feature boundaries; container release validation covers both Linux architectures. Reverified the integrated main tree with lint, formatting, tests, and type checking.
<!-- SECTION:FINAL_SUMMARY:END -->
