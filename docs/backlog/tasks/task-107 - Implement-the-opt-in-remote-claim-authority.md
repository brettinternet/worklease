---
id: TASK-107
title: Implement the opt-in remote claim authority
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
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
The Go release coordinates cooperating agents only on one host. `docs/remote-claim-authority.md` records the owner decisions of 2026-09-12 and 2026-09-13, and contract section 20 was amended under section 15 to match: an opt-in, self-hosted remote authority that serves the same `lease.Service` and SQLite store through `worklease serve` over authenticated HTTPS, with the shell CLI and the local stdio MCP adapter as clients and guarded effects always executing on the client host.

This parent authorizes the implementation slice the design calls initial: one namespace per `serve` process; portable keys only, with admitted prefixes and TTL/hold bounds in a deployment-owned server configuration file; invite-based enrollment with client-generated installation credentials and `read`/`write`/`admin` roles; an immutable `expectedRestoreId` in every authenticated request with `restoreId` and `authorityTime` on every response; a namespace recovery mode used by restore and reopened by an operator attestation; the hosted single-writer OS lock; same-host transfer; pending state as the client recovery evidence; and the shared schema bump these require. The named follow-ups (repository enrollment and aliases, cross-host `transfer-prepare`, audited recovery import, a durable client journal, admission backpressure, browser login or a control plane, multi-namespace `serve`, Postgres) are out of scope and must not be pulled in without a recorded trigger.

Subtasks deliver in dependency order. Worklease ships the capability, not an operated service: no high-availability claim is made, and promotion to an experimental release requires the two-host acceptance harness to pass. Until this parent closes, the standard release must keep opening no listener and making no network request unless a remote profile or `serve` is explicitly used.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every subtask is Done with its acceptance evidence recorded in the task.
- [ ] #2 One `worklease` binary runs `serve` on one host while two other hosts with different checkout roots contend on one portable key through both the CLI and the MCP adapter, and every guarded effect executes only on a client host.
- [ ] #3 The standard release opens no listener and performs no network request unless a remote profile or `serve` is explicitly used; local-only behavior, the local test suite, and `mise run ci` are unchanged apart from the shared schema bump.
- [ ] #4 `docs/remote-claim-authority.md` and contract section 20 (through the section 15 procedure) describe the shipped experimental capability, and the follow-up list still records every deferred item with its trigger.
<!-- AC:END -->
