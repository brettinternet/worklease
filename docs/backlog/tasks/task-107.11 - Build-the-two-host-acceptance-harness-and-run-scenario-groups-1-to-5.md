---
id: TASK-107.11
title: Build the two-host acceptance harness and run scenario groups 1 to 5
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 15:30'
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
modified_files:
  - cmd/worklease-remote-smoke/main.go
  - cmd/worklease-remote-smoke/main_test.go
  - internal/authority/authority.go
  - internal/authority/http.go
  - internal/guard/guard.go
  - internal/guard/remote_test.go
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Keep the checked-in local-development probe as the fast TLS/two-root CLI/MCP/guard/backup/restore path in e2e.
2. Add deployment-owned real-host orchestration around the same scenario operations, including independent fault dispatch/effect counters and asynchronous backup selection.
3. Complete every Group 1-5 case against one authority and two real client hosts; retain redacted commands, environment, measurements, cutoffs, recovery bounds, and owning observations.
4. Rerun mise run ci and the unchanged real-host harness, review failures, and close only after all objective evidence passes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the local-development acceptance probe in cmd/worklease-remote-smoke and wired it into scripts/test-e2e.sh plus mise run remote-smoke. The probe launches the shipped TLS server, creates two isolated checkout/config/credential/home roots, exercises CLI contention, stdio MCP, a client-local guarded effect with dispatch count 1, partition fail-closed behavior, enrollment/credential failure, snapshot/watch, and an online SQLite backup followed by restore. It writes owner-private command, measurement, backup, pending-inventory, and per-group report evidence under dist/remote-acceptance/TIMESTAMP/.

Local evidence: dist/remote-acceptance/20260914T114650.483809000Z/report.json. mise run ci passed on 2026-09-14. This is explicitly development evidence only: the complete fault matrix and unchanged real-host run remain blocking, including measured WAN latency/throughput, real host/process/filesystem boundaries, external asynchronous provider effects, and deployment backup/restore bounds.

Committed local-development harness as a87b3f0 (Add remote authority development smoke).

Extended cmd/worklease-remote-smoke with an opt-in --remote-host topology. A real run used the local machine as client A/orchestrator and an SSH host as the TLS authority plus client B, with separate checkout, config, credential, home, and pending roots. The runner copies owner-private binaries/TLS material, chooses a port on the authority host, exercises remote CLI and stdio MCP, measures end-to-end remote-command latency, performs an online authority-host SQLite backup and restore, preserves exact dispatch counts, and retains an owner-marked remote workspace instead of deleting it.

Real-host evidence: dist/remote-acceptance/20260914T141254.154954000Z/report.json and coverage.json; retained workspace remote-host:/private/tmp/worklease-acceptance-iulAlh. The explicit AC #3-#7 matrix currently records 6 live passes, 1 supporting-test pass, and 42 still-blocked clauses, so this evidence does not close the task. mise run ci passed after the real-host run. Reviewer automation was incomplete due its read budget; direct review fixed remote port selection, report latency labeling, append-only server logs, and local/remote evidence classification.

Committed the real-host smoke and explicit coverage matrix as d95b6c8 (Run remote smoke across two hosts).

Handoff / next resumable step: start with coverage entries AC3.3-AC3.5 in cmd/worklease-remote-smoke: add real-host raw and reserved-prefix rejection, rewrite the remote server configuration and verify changes apply only after restart, then exercise persisted TTL/hold limits through acquire, heartbeat, begin/renew operation, and same-host transfer. Rerun with --remote-host remote-host and update coverage.json only from objective observations. Continue in coverage order; 42 entries remain still-blocked. The remote-host workspaces are intentionally owner-marked and retained; do not infer cleanup ownership from names.

Takeover started under Worklease claim session task-107-11-takeover. Continuing from AC3.3 in coverage order; authoritative provider and dependencies revalidated.

Completed the live Group 1 admission/configuration slice. The unchanged harness now rejects raw host-local resources, rejects a reserved-prefix server configuration at startup, proves config rewrites do not affect the running process until restart, enforces a reduced TTL ceiling for new claims, and proves the originally persisted claim limits continue through heartbeat, guarded exec, and transfer. Also normalized explicit evidence paths to absolute paths so guarded effects are independent of client checkout cwd.

Objective evidence: local report dist/remote-acceptance/ac3-local-test-4/report.json; real-host report dist/remote-acceptance/20260914T144421.181833000Z/report.json and coverage.json; retained workspace remote-host:/private/tmp/worklease-acceptance-eZVAQp. AC3.3-AC3.5 are live-pass. Quality gates mise run lint, format-check, test, and typecheck passed.

Committed Group 1 admission/configuration acceptance as b0cb24a (Expand remote admission acceptance).

Extended the real-host harness with credential rotation/revocation and hosted-lock boundary observations. Group 3 now rotates the worker through an owner-private hidden invite, revokes the old installation, proves the old bearer returns installation-revoked, and proves the replacement bearer remains usable. Group 5 now proves direct local mutation is refused against a marked hosted home while the lock is free, and proves second serve, bootstrap reissue, and retire are refused while the server holds the lock.

Objective evidence: local reports dist/remote-acceptance/group3-local-test/report.json and lock-local-test/report.json; real-host report dist/remote-acceptance/20260914T145035.814268000Z/report.json and coverage.json; retained workspace remote-host:/private/tmp/worklease-acceptance-tj1wkd. AC5.7, AC5.8, AC7.19, and AC7.20 are live-pass. Quality gates lint, format-check, test, and typecheck passed.

Committed credential and hosted-lock acceptance as e6d9314 (Exercise remote credential and lock boundaries).

Next resumable step: implement transport fault injection for AC4.1 and AC4.4-AC4.8, beginning with lost start/renewal/completion responses and exact replay. The latest matrix still intentionally leaves every unobserved clause still-blocked; do not close the task from supporting tests.

Resumed under Worklease claim task-107-11-loop at AC4.1 transport fault injection; dependencies and prior evidence revalidated.

Implemented AC4.1 one-shot TLS response-loss injection for begin, renewal, and completion. The harness records request-body hashes and requires an identical replay; lost begin remains unknown with zero dispatch, while renewal and completion replay with one effect. Acceptance exposed and fixed secondary pending replay sourcing the installation bearer instead of the contextual claim handle, completion replay being bypassed by begin replay, replayed JSON-number exit codes, and a finished-child/in-flight renewal race. Local objective evidence: dist/remote-acceptance/ac4-local-test-8/report.json and fault-proxy.log. A post-review real-host rerun is temporarily blocked by repeated baseline remote-host transport unknown-outcome failures before Group 2; the earlier pre-review run passed but is not used as final AC4.1 evidence.

Committed AC4.1 transport-fault acceptance and replay fixes as fd58e45. Next resumable step is AC4.4 race ordering for installation revocation and policy changes. Post-review real-host rerun still requires a stable remote-host transport; repeated baseline setup mutations returned unknown-outcome before the fault slice.
<!-- SECTION:NOTES:END -->
