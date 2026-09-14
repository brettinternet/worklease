---
id: TASK-107.11
title: Build the two-host acceptance harness and run scenario groups 1 to 5
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:03'
labels:
  - remote-authority
dependencies:
  - TASK-107.5
  - TASK-107.7
  - TASK-107.9
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
Build a reproducible acceptance harness and run all five scenario groups against one TLS authority host and two client hosts with distinct roots. Fixtures must exercise the real client, server, shared schema, CLI, MCP adapter, local guarded effects, asynchronous backup selection, restore, and offline lock boundary. Fault injection records expected effects and invocation counts so replay and late responses cannot hide duplicate execution. Local containers or VMs support development, but promotion requires the same harness to pass on real hosts.

Report simulated WAN latency separately from measured real-host latency. Record exact evidence paths, commands, environment, measurements, selected backup cutoffs, unknown recovery bounds, and the observation for each scenario group. Unit tests support the harness but do not substitute for real WAN, backup, restore, filesystem, or process behavior. Every failure blocks Done until fixed and rerun.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A reproducible harness provisions one authority behind TLS and two client hosts with distinct checkout and credential roots, runs both CLI and MCP paths, and captures expected external effects and exact dispatch counts under injected faults.
- [ ] #2 The harness runs locally in containers or VMs for development and unchanged on real hosts for acceptance. Reports label synthetic WAN injection separately from real-host measurements and record commands, evidence paths, environment, latency, renewal margins, throughput, storage behavior, backup cutoff, restore time, and recovery bounds.
- [ ] #3 Group 1 covers cross-host contention and separate scopes, repository-independent profile selection, raw and misconfigured reserved-prefix rejection, configuration restart behavior, and persisted admission limits on every extension path.
- [ ] #4 Group 2 covers exact replay after lost start, renewal, and completion responses; coexistence of request-scoped recovery records with original guarded-effect evidence; no local fallback during partition; race ordering for revocation and policy changes; fresh response identity and time; clock-bound edge cases; pre-dispatch persistence failure; late acknowledgment without redispatch; and an asynchronous provider effect that continues after terminal completion.
- [ ] #5 Group 3 covers bootstrap crash ordering and redaction, hidden/file/descriptor invite input, dropped invite and redemption responses, immutable request incarnation, no-burn mismatch, role isolation, rotation, revocation, and distinct MCP authentication guidance.
- [ ] #6 Group 4 covers snapshot/watch races, disconnect and reconnect, cursor incarnation and retention gaps, stuck-history retention, full-volume `storage-failure` without pruning, and client pending evidence surviving age, GC, replay expiry, restart, and profile changes.
- [ ] #7 Group 5 uses an actual asynchronous backup fixture with chosen and older cutoffs. It covers zero and nonzero pending sets, restart, schema and protocol upgrade, restored and missing credentials, double restore, retained start with lost completion, a known confirmed start missing from the backup while its client is offline or incomplete, fully missing completed work, provider effects after terminal receipt, installation inventories including ephemeral and retired clients, missing evidence that blocks reopening, explicitly unknown cutoffs or history bounds with otherwise exhaustive coverage, transitive closure including the over-32 failure, retained replay, bootstrap reissue, atomic reopen, every lock-held bypass attempt, and a direct local mutation refused against a marked hosted home while the lock is free.
- [ ] #8 Each scenario group records its owning observation and objective pass evidence. Any failure remains blocking and the task cannot close until it is fixed and the affected scenario passes; unit coverage does not replace the real-host run.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
