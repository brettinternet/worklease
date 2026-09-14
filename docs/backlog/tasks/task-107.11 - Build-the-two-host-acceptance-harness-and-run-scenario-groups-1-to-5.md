---
id: TASK-107.11
title: Build the two-host acceptance harness and run scenario groups 1 to 5
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies:
  - TASK-107.10
references:
  - scripts/test-e2e.sh
  - cmd/worklease-smoke
  - docs/reviews
  - TASK-85.18
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 143000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The design defers every reliability claim until executable evidence exists from a real deployment: two client hosts with different checkout roots and one `serve` host behind a TLS edge, exercising the five scenario groups listed under Remaining decisions and release evidence in `docs/remote-claim-authority.md` (identity and admission; lost response, partition, and authority time; enrollment and roles; watches and retention; restart, upgrade, and restore including double restore and the R0 to R1 stale request). Unit tests cannot substitute for WAN latency, replica restore, and paused-process behavior. The harness should be reproducible from `mise` with local containers or VMs and runnable unchanged against real hosts, and it should collect the measurements the design says decide later follow-ups.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A reproducible harness provisions one `serve` host with TLS and two client hosts, runs the CLI and the MCP adapter from both clients, and records WAN latency, renewal margins, write throughput, replication lag, and restore time.
- [ ] #2 Scenario group 1 passes: cross-host contention on one portable key, separate scopes not contending, no repository-driven profile selection, reserved prefixes rejected through raw input and a bad allowlist, prefix withdrawal and bound changes affecting only new admissions, and persisted limits capping every extension path.
- [ ] #3 Scenario group 2 passes: lost-response recovery by exact replay, a partition stopping new client effects with no local fallback, revocation and policy changes raced against mutations with the frozen check order, replay wrapped in fresh authority time, and the authority-time edge cases named in the design.
- [ ] #4 Scenario group 3 passes: first-start bootstrap file, hidden and file and descriptor invite input without disclosure, dropped issuance and redemption responses, incarnation mismatch with no burn, immutable ids, rotation, role isolation, and distinct MCP guidance reasons.
- [ ] #5 Scenario groups 4 and 5 pass: watch and snapshot races and cursor gaps, full-volume `storage-failure` with nothing pruned, restart preserving `restoreId`, replica restore with `restored` claim ends and revoked rows, double restore, an R0 pending acquire rejected under R1, a lost confirmed start held in client pending state until attested, a completed lost-tail operation recorded as an audit gap, transitive recovery closure, retained replay, bootstrap reissue, atomic reopening, and paused-server lock behavior.
- [ ] #6 Results, measurements, and deviations are recorded in the task and in a `docs/reviews/` report; every failure is fixed or explicitly recorded as a blocker before this task closes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
