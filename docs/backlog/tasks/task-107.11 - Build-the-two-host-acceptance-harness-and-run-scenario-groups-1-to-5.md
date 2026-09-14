---
id: TASK-107.11
title: Build the two-host acceptance harness and run scenario groups 1 to 5
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 22:24'
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
  - internal/handle/handle.go
  - internal/handle/handle_test.go
  - internal/lease/remote_auth_acceptance_test.go
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

5. Add objective AC4.8 late-acknowledgment coverage that proves a retained start acknowledgment never redispatches the guarded effect, then add the next coherent Group 2 slice if the existing lifecycle supports it; run local and real-host harnesses, review, quality gates, and commit task evidence.

6. Implement the next coherent Group 3 enrollment slice: AC5.1 bootstrap crash/redaction, AC5.2 alternate invite inputs, AC5.3 dropped invite/redemption replay, AC5.4 immutable request incarnation, and AC5.5 no-burn mismatch; add objective local/real-host evidence where reachable, review, run quality gates, and commit.

7. Preserve AC5.1, the hidden-prompt portion of AC5.2, and AC5.4 as explicit blockers rather than overstating this slice; commit the completed AC5.3 and AC5.5 evidence, then resume with retained enrollment evidence across restore.

8. Retain a dropped enrollment pending record across restore for AC5.4, then implement the reachable bootstrap crash/redaction and hidden invite-input portions of AC5.1-AC5.2; preserve any genuinely unreachable clauses as explicit blockers, run local and real-host evidence where available, review, run all quality gates, and commit.

9. Replace personal SSH-host assumptions with a repository-managed Lima VM for durable remote acceptance runs; keep the generic SSH target supported for deployment-owned environments.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the local-development acceptance probe in cmd/worklease-remote-smoke and wired it into scripts/test-e2e.sh plus mise run remote-smoke. The probe launches the shipped TLS server, creates two isolated checkout/config/credential/home roots, exercises CLI contention, stdio MCP, a client-local guarded effect with dispatch count 1, partition fail-closed behavior, enrollment/credential failure, snapshot/watch, and an online SQLite backup followed by restore. It writes owner-private command, measurement, backup, pending-inventory, and per-group report evidence under dist/remote-acceptance/TIMESTAMP/.

Local evidence: dist/remote-acceptance/20260914T114650.483809000Z/report.json. mise run ci passed on 2026-09-14. This is explicitly development evidence only: the complete fault matrix and unchanged real-host run remain blocking, including measured WAN latency/throughput, real host/process/filesystem boundaries, external asynchronous provider effects, and deployment backup/restore bounds.

Committed local-development harness as a87b3f0 (Add remote authority development smoke).

Extended cmd/worklease-remote-smoke with an opt-in --remote-host topology. A real run used the local machine as client A/orchestrator and an SSH host as the TLS authority plus client B, with separate checkout, config, credential, home, and pending roots. The runner copies owner-private binaries/TLS material, chooses a port on the authority host, exercises remote CLI and stdio MCP, measures end-to-end remote-command latency, performs an online authority-host SQLite backup and restore, preserves exact dispatch counts, and retains an owner-marked remote workspace instead of deleting it.

Real-host evidence: dist/remote-acceptance/20260914T141254.154954000Z/report.json and coverage.json; retained workspace remote-host:/private/tmp/worklease-acceptance-iulAlh. The explicit AC #3-#7 matrix currently records 6 live passes, 1 supporting-test pass, and 42 still-blocked clauses, so this evidence does not close the task. mise run ci passed after the real-host run. Reviewer automation was incomplete due its read budget; direct review fixed remote port selection, report latency labeling, append-only server logs, and local/remote evidence classification.

Committed the real-host smoke and explicit coverage matrix as d95b6c8 (Run remote smoke across two hosts).

Handoff / next resumable step: start with coverage entries AC3.3-AC3.5 in cmd/worklease-remote-smoke: add real-host raw and reserved-prefix rejection, rewrite the remote server configuration and verify changes apply only after restart, then exercise persisted TTL/hold limits through acquire, heartbeat, begin/renew operation, and same-host transfer. Rerun with --remote-host lima-worklease-remote and update coverage.json only from objective observations. Continue in coverage order; 42 entries remain still-blocked. The remote host workspaces are intentionally owner-marked and retained; do not infer cleanup ownership from names.

Takeover started under Worklease claim session task-107-11-takeover. Continuing from AC3.3 in coverage order; authoritative provider and dependencies revalidated.

Completed the live Group 1 admission/configuration slice. The unchanged harness now rejects raw host-local resources, rejects a reserved-prefix server configuration at startup, proves config rewrites do not affect the running process until restart, enforces a reduced TTL ceiling for new claims, and proves the originally persisted claim limits continue through heartbeat, guarded exec, and transfer. Also normalized explicit evidence paths to absolute paths so guarded effects are independent of client checkout cwd.

Objective evidence: local report dist/remote-acceptance/ac3-local-test-4/report.json; real-host report dist/remote-acceptance/20260914T144421.181833000Z/report.json and coverage.json; retained workspace remote-host:/private/tmp/worklease-acceptance-eZVAQp. AC3.3-AC3.5 are live-pass. Quality gates mise run lint, format-check, test, and typecheck passed.

Committed Group 1 admission/configuration acceptance as b0cb24a (Expand remote admission acceptance).

Extended the real-host harness with credential rotation/revocation and hosted-lock boundary observations. Group 3 now rotates the worker through an owner-private hidden invite, revokes the old installation, proves the old bearer returns installation-revoked, and proves the replacement bearer remains usable. Group 5 now proves direct local mutation is refused against a marked hosted home while the lock is free, and proves second serve, bootstrap reissue, and retire are refused while the server holds the lock.

Objective evidence: local reports dist/remote-acceptance/group3-local-test/report.json and lock-local-test/report.json; real-host report dist/remote-acceptance/20260914T145035.814268000Z/report.json and coverage.json; retained workspace remote-host:/private/tmp/worklease-acceptance-tj1wkd. AC5.7, AC5.8, AC7.19, and AC7.20 are live-pass. Quality gates lint, format-check, test, and typecheck passed.

Committed credential and hosted-lock acceptance as e6d9314 (Exercise remote credential and lock boundaries).

Next resumable step: implement transport fault injection for AC4.1 and AC4.4-AC4.8, beginning with lost start/renewal/completion responses and exact replay. The latest matrix still intentionally leaves every unobserved clause still-blocked; do not close the task from supporting tests.

Resumed under Worklease claim task-107-11-loop at AC4.1 transport fault injection; dependencies and prior evidence revalidated.

Implemented AC4.1 one-shot TLS response-loss injection for begin, renewal, and completion. The harness records request-body hashes and requires an identical replay; lost begin remains unknown with zero dispatch, while renewal and completion replay with one effect. Acceptance exposed and fixed secondary pending replay sourcing the installation bearer instead of the contextual claim handle, completion replay being bypassed by begin replay, replayed JSON-number exit codes, and a finished-child/in-flight renewal race. Local objective evidence: dist/remote-acceptance/ac4-local-test-8/report.json and fault-proxy.log. A post-review real-host rerun is temporarily blocked by repeated baseline remote host transport unknown-outcome failures before Group 2; the earlier pre-review run passed but is not used as final AC4.1 evidence.

Committed AC4.1 transport-fault acceptance and replay fixes as fd58e45. Next resumable step is AC4.4 race ordering for installation revocation and policy changes. Post-review real-host rerun still requires a stable remote host transport; repeated baseline setup mutations returned unknown-outcome before the fault slice.

Implemented deterministic AC4.4 ordering gates in the shared local/real-host harness. The TLS fault proxy can now hold an acquire before forwarding and records matched request-hash hold/release evidence. Group 2 proves installation revocation serialized first rejects a held mutation, a mutation committed first remains retained after revocation, prefix withdrawal serialized first rejects a held admission, and a claim admitted first can heartbeat after withdrawal. Held client commands are process-bounded. Local objective evidence: dist/remote-acceptance/ac4-race-local-test-3/report.json, coverage.json, fault-proxy.log, and race-ordering.txt. Focused tests and the full local harness pass. The real-host run remains blocked before Group 2 by the pre-existing remote host baseline invite-issue unknown-outcome during provisioning; no AC4.4 real-host pass is claimed. Reviewer findings for SSH argument splitting, stale copied proxy evidence, and unbounded held commands were fixed.

Committed AC4.4 ordering acceptance as d77735a (Exercise remote policy race ordering). Independent review found three concrete harness defects; all were fixed before commit and the reviewer reported no additional findings. Next resumable step: AC4.5 fresh response identity and authority time. The remote host provisioning transport must be stable before any new real-host coverage can be promoted.

Resumed under Worklease claim task-107-11-loop-2 at AC4.5 fresh response identity and authority time; dependencies, prior evidence, and clean main checkout revalidated.

Implemented AC4.5 fresh replay envelopes. The fault proxy now records redacted authorityId, restoreId, authorityTime, and a canonical historical-result hash for application responses. Group 2 requires an exact completion replay to retain the historical result and current authority/restore identity while advancing authority time. Local objective evidence: dist/remote-acceptance/20260914T155938.260367000Z/report.json, coverage.json, and fault-proxy.log; AC4.5 is local live-pass. The real-host rerun reached Group 1 but remote host again returned a baseline heartbeat unknown-outcome before Group 2, so no real-host AC4.5 pass is claimed. Quality gates lint, format-check, test, and typecheck passed. Next resumable step: AC4.6 clock-bound edge cases; rerun AC4.5 on remote host when baseline transport is stable.

Committed AC4.5 acceptance as 8addab9 (Verify fresh remote replay envelopes).

Resumed under Worklease claim task-107-11-loop-3 at AC4.6 clock-bound edge cases; dependencies, prior evidence, and clean main checkout revalidated.

Implemented AC4.6 authority-time acceptance. Remote default request deadlines now come from the sampled authority lower bound rather than client wall time; the client counts asymmetric response latency and wall elapsed across suspend, and refuses a newly acknowledged guarded effect at the three-quarter-TTL stop-new-work boundary. The TLS fault proxy injects a 1.2s asymmetric response delay plus a one-hour authority/client skew, proves the wire deadline is the sampled lower bound plus 24h, proves an expired short window sends nothing, and proves a delayed successful begin dispatches no effect. Local objective evidence: dist/remote-acceptance/ac4-clock-local-test-6/report.json, coverage.json, fault-proxy.log, and clock-bounds.txt; AC4.6 is local live-pass. The real-host rerun remains blocked during baseline provisioning by remote host invite issuance returning unknown-outcome before Group 2. Independent review found suspend elapsed-time, three-quarter-TTL, and false-positive deadline-evidence defects; all were fixed before commit. Next resumable step: AC4.7 pre-dispatch persistence failure, with a remote host rerun when baseline transport is stable.

Committed AC4.6 as 04bbd1e (Enforce remote authority time bounds). Quality gates mise run lint, format-check, test, typecheck, ci, and staged hooks passed.

Resumed on main at AC4.7 pre-dispatch persistence failure; dependencies and prior evidence revalidated.

Implemented AC4.7 pre-dispatch persistence failure. The harness replaces client A's pending root with a regular file, arms a pre-forward /v1/admin/gc proxy gate, requires storage-failure, proves the gate remained armed and no GC request appears in the proxy log, then restores the pending directory. Local objective evidence: dist/remote-acceptance/ac4-persistence-local-test-3/report.json, coverage.json, fault-proxy.log, and pre-dispatch-persistence.txt. A real-host rerun reached and passed this slice with evidence under dist/remote-acceptance/ac4-persistence-real-test/ before the existing later race-client enrollment credential-unsafe failure; no complete real-host report is claimed. Independent verification passed focused tests, local smoke, and this real-host slice. Quality gates lint, format-check, test, and typecheck passed. Next resumable step: AC4.8 late acknowledgment without redispatch.

Committed AC4.7 as dc2dbba (Verify pre-dispatch persistence failure). Staged hooks passed.

Implemented AC4.8 and AC4.9. Group 2 now separately requires a successful undropped late start acknowledgment beyond the safe dispatch window plus client-side refusal and zero dispatch, and combines it with exact retained-start replay evidence. Added a managed asynchronous-provider fixture: guarded exec submits one atomically unique provider request, reaches terminal completion, then the harness releases the provider; an append-only uniquely identified completion log must contain exactly one effect after the receipt barrier. Local objective evidence: dist/remote-acceptance/ac4-late-provider-local-final/report.json, coverage.json, late-acknowledgment.txt, asynchronous-provider-effect.txt, provider-submitted.txt, and provider-completed.log. The unchanged real-host run also passed both new slices with partial evidence under dist/remote-acceptance/ac4-late-provider-real-test-2/ before the pre-existing race-client enrollment credential-unsafe failure, so no complete real-host report is claimed. Two independent reviews found false-pass risks in HTTP acknowledgment validation, dispatch counting, receipt ordering, worker cleanup, and duplicate submissions; all were fixed. Quality gates lint, format-check, test, and typecheck passed. Next resumable step: AC5.1 bootstrap crash ordering and redaction.

Committed AC4.8 and AC4.9 as d8c0580 (Exercise late and asynchronous effects). Staged hooks passed.

Implemented the next reachable Group 3 enrollment fault slices. AC5.3 now drops and exactly replays invite issuance and descriptor-based enrollment responses, requiring identical request and historical result hashes. AC5.5 now sends a wrong restore incarnation, snapshots the complete installation inventory before/after, proves the invite remains redeemable, scans evidence for invite and generated credential disclosure, and adds direct service coverage for zero invite/redemption/installation mutation. AC5.4 remains still-blocked because the harness has not yet retained enrollment pending evidence across an actual restore; AC5.1 and the hidden-prompt portion of AC5.2 also remain blocked.

Objective local evidence: dist/remote-acceptance/ac5-enrollment-local-final-2/report.json, coverage.json, fault-proxy.log, enrollment-replay.txt, enrollment-incarnation-mismatch.json, and post-mismatch-installations.json. The unchanged real-host attempt at dist/remote-acceptance/ac5-enrollment-real-test/ was blocked before Group 3 by the existing remote host race-client enrollment credential-unsafe failure. Independent review findings for result verification, mismatch credential redaction, remote-helper misuse, full inventory comparison, and overstated AC5.4 coverage were fixed. Quality gates and final CI are rerun before commit. Next resumable step: retain a dropped enrollment pending record across restore for AC5.4, then add bootstrap crash/redaction and hidden invite input for AC5.1-AC5.2.

Final CI exposed an intermittent descriptor-enrollment failure. Root cause: ReadCredentialFD both closed the duplicated descriptor directly and left an owning os.File for finalization, allowing a later finalizer to close a reused descriptor. The descriptor now has one os.File owner and closes exactly once; added regression coverage. This fix also addresses the recurring real-host credential-unsafe symptom's descriptor-lifetime class, though the remote host harness must still be rerun.

Committed AC5.3 and AC5.5 slices as f07733b (Exercise remote enrollment replay faults). Final verification passed: mise run lint, format-check, test, typecheck, ci, and staged hooks. Local smoke passed repeatedly after the descriptor ownership fix, including dist/remote-acceptance/ac5-descriptor-fix-2/report.json and CI evidence dist/remote-acceptance/20260914T221359.558406000Z/report.json.
<!-- SECTION:NOTES:END -->
