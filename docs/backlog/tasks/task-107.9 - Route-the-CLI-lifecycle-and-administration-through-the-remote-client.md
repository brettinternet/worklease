---
id: TASK-107.9
title: Route the CLI lifecycle and administration through the remote client
status: Done
assignee:
  - '@pi'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 10:47'
labels:
  - remote-authority
dependencies:
  - TASK-107.8
references:
  - internal/cli
  - internal/guard
  - internal/doctor
  - TASK-85.12
  - TASK-85.14
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 141000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Route existing CLI lifecycle and supported administration through the shared authority interface. Keep all effects on the client host. Remote guarded `exec` belongs in `internal/guard/guard.go` and `internal/cli/guard_commands.go`: it begins, renews, and completes the operation through the remote authority while supervising the local process group. `replace-file` remains unsupported for remote profiles because its replacement boundary is local. No client callback, child process, provider request, or SQL callback executes through server HTTP.

Renew at half TTL and stop admitting new local guarded work at three quarters. Once termination is required, begin bounded process-group termination immediately on confirmed ownership, authentication, or incarnation loss, or no later than the conservative known lease expiry when renewal remains uncertain. Do not claim that termination completes at three quarters or that a lease revision fences an escaped process or asynchronous provider. Late responses and replay recover state but never cause effect redispatch.

CLI administration covers API invite issuance, installation revocation, administrative claim revocation, reopening with its bounded private attestation, private inspection, and GC. Offline initialization, restore, bootstrap reissue, and retirement remain offline commands under the hosted lock.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Existing CLI acquire, heartbeat, checkpoint, release, same-host transfer, status, list, events, history, watch, verify, inspection, and reconciliation use the shared local or remote authority interface while handles, claim credentials, and pending records remain on the client host.
- [x] #2 Remote guarded `exec` runs the child only on the client, renews at half TTL, stops new guarded work at three quarters, and begins bounded process-group termination immediately on confirmed ownership, authentication, or incarnation loss or by conservative known expiry when confirmation remains unavailable.
- [x] #3 Guard output does not claim fencing or guaranteed cessation. Async provider work and escaped descendants remain subject to explicit outcome and cessation evidence even after a terminal receipt or empty pending set.
- [x] #4 Replay, dropped start, renewal, or completion responses, and late acknowledgments recover the original request without redispatching the local effect. Renewal and completion recovery records coexist with the original guarded start and effect evidence and cannot overwrite or prematurely clear it; a retained started result remains unknown until terminal completion or reconciliation.
- [x] #5 Remote `replace-file` returns the frozen unsupported reason and performs no local or remote write. `BeginOperation` may carry bounded private argv for hashing and authorized ledger inspection, but the server never receives or executes an effect callback, provider dispatch, file replacement callback, or SQL callback.
- [x] #6 API admin commands implement invitation, installation and claim revocation, bounded private inspection, GC, and typed reopen. Offline init, restore, bootstrap reissue, and retirement are not HTTP administration.
- [x] #7 Reopen submits the complete structured attestation and remains refused when retained reconciliation, inventory, pending-set or equivalent coverage, lost-tail outcomes, or namespace-wide cessation coverage is missing. An unknown selected cutoff or history bound is recorded as unknown and does not excuse missing coverage.
- [x] #8 Remote `doctor` reports redacted credential presence without loading the secret for that check. Its authenticated reachability, current authority and restore identity, and recovery-mode checks load and use the credential normally without exposing it. Invite issuance writes the raw code only to a protected file or hidden secret channel and never to argv, ordinary output, or logs.
- [x] #9 Existing local guarded-execution and replace-file tests remain unchanged and pass.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Introduce one profile-aware CLI authority context that selects local or remote without fallback and keeps remote handles/pending requests client-local.
2. Route lifecycle, ledger, watch, verification, and reconciliation commands through authority.Authority while preserving local behavior and exact remote replay.
3. Generalize guarded exec over the authority contract, enforce conservative renewal/termination, and reject remote replace-file before any file effect.
4. Add remote profile/enrollment/admin/recovery/GC commands and profile-aware doctor checks with protected secret handling.
5. Add focused remote CLI, guard, admin, and doctor tests; run unchanged local guard/replace tests and all repository quality gates.
6. Review acceptance-critical safety boundaries, fix findings, finalize Backlog evidence, and commit on main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Selected as the earliest dependency-ready high-priority item. Claimed with Worklease local coordination; providerMutationFenced=false. Discovery confirmed TASK-107.8 supplied the authority client foundation while CLI consumers remain local-only.

Implemented profile-aware CLI authority construction and routed lifecycle, ledger, watch, verification, inspection, reconciliation, transfer, and guarded exec through the local/remote authority contract. Added client-local remote handle/pending behavior, exact request deadline reuse, child-operation pending coexistence/replay, remote replace-file refusal, profile/enrollment/admin/recovery/GC commands, and remote doctor checks. Added real hosted-server CLI coverage for acquire/status/list/events/history/watch/verify/checkpoint/heartbeat/transfer/exec/release, doctor redaction, invite secret handling, and replace refusal. Focused and full Go tests pass; staticcheck issue from an obsolete local watch helper was removed.

Validation: mise run ci passed on commit 8a59963, including staticcheck, gofmt check, go vet, full tests, race tests, govulncheck, generated-man check, and end-to-end smoke/docs. Focused hosted-server tests exercise remote lifecycle/read routing, local-only effect placement, transfer, guarded exec, interrupted-exec inspection/reconciliation, invite secret protection, doctor credential redaction/authentication/recovery checks, and remote replace-file refusal. Existing local guard and replace-file suites pass unchanged. Diff review found and fixed stale remote handle revisions, application-error ambiguity classification, exact deadline replay drift, and renewal/completion pending-record collisions.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Routed the shell CLI through the shared local/remote authority abstraction, added client-local remote lifecycle and guarded-exec recovery, profile/enrollment and API administration commands, safe remote replace-file refusal, and profile-aware doctor diagnostics. Verified with real hosted-server CLI integration tests and the full mise run ci gate on implementation commit 8a59963.
<!-- SECTION:FINAL_SUMMARY:END -->
