---
id: TASK-85.7
title: Implement the claim lifecycle service
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.5
  - TASK-85.6
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/claims.py
  - src/worklease/acquisition.py
  - src/worklease/lifecycle.py
  - src/worklease/operations.py
  - src/worklease/models.py
  - tests/test_store.py
  - tests/test_cli.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 99000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
This is the core: acquire, status, list, heartbeat, checkpoint, release, transfer, lazy expiry, idempotent operations, contention waits, and the clock rules over the new store, exposed as `internal/lease.Service` and as the first lifecycle CLI commands. Under the one-claim model (D4) the data model and service already carry an ordered resource set; this task implements everything for claims of exactly one resource and rejects more with a clear error until TASK-85.8 lifts the limit. Handles arrive in TASK-85.10, so the CLI in this task accepts explicit credentials only and prints the token in JSON when acquiring.

Read first: contract sections 2 (D4 to D7, D9, D14), 4 (acquire, status, list, heartbeat, checkpoint, release, transfer rows; explicit claim selection only for now), 6, 7.1 to 7.9, 7.11, 7.12, 8, and 18 (lease sketch). Python evidence: `src/worklease/claims.py`, `acquisition.py` (singleton path), `lifecycle.py` (transfer and release), `operations.py` (ledger, request hash, replay, mismatch, unknown outcome), `models.py` (bounds); `tests/test_store.py`: test_expiry_reclaim_replaces_token_and_increases_revision, test_small_backward_clock_step_keeps_a_live_lease, test_backward_clock_regression_is_reanchored_by_a_contender, test_forward_clock_step_past_expiry_still_expires, test_checkpoint_renews_replays_and_rejects_stale_owner, test_transfer_replaces_owner_atomically_and_preserves_checkpoint, test_transfer_rolls_back_on_interruption_without_free_interval, test_transfer_serializes_contender_acquire, test_acquire_retry_rejects_any_ttl_change, test_stale_owner_cannot_heartbeat_after_reclaim, test_heartbeat_retry_is_idempotent_and_rejects_request_mismatch, test_release_retry_is_idempotent_and_claim_id_cannot_be_reused, test_claim_survives_process_exit_and_can_be_reclaimed, test_invalid_ttl_and_blank_release_reason_do_not_change_state; `tests/test_cli.py`: test_wait_retries_transient_contention_until_acquire, test_no_wait_remains_one_atomic_attempt, test_wait_timeout_preserves_conflict_exit_code_and_redaction, test_heartbeat_distinguishes_invalid_token_from_stale_claim, test_acquire_defaults_generate_and_echo_identifiers, test_lifecycle_redacts_read_only_tokens_and_supports_text_list.

Deliver in `internal/lease`: `Service` with `New`, `Acquire` (validation, expired-holder replacement with the `expired-replaced` event and epoch finalization, clock-regression re-anchoring, contention detail, `--wait` loop with jittered polling on monotonic time and context cancellation), `Status` by resources or claim id, `List`, `Heartbeat`, `Checkpoint` (JSON up to 8 KiB), `Release`, `Transfer` (successor at revision 1, no free interval), `BeginOperation` and `CompleteOperation` for guarded intents (used by TASK-85.12), the request-hash and replay rules of contract 7.4, the credential checks of 7.3 in order with constant-time token comparison, and every event of 7.12 appended in the same transaction. Every mutation writes exactly one operations row. Deliver in `internal/cli`: `acquire` (single `-r`, `--ttl`, `--wait`, `--poll-interval`, `--agent`, `--session`, `--work-key`, `--claim-id`, `--coordination-only`; `--no-handle` accepted and required until TASK-85.10; JSON prints the token), `status`, `list`, `heartbeat`, `checkpoint`, `release`, `transfer` with explicit `--claim-id`, `--revision`, `--token-file`, `--token-fd` selection; contention output shows the holder's non-secret metadata and a wait hint.

Owned paths: `internal/lease`, `internal/cli/acquire.go`, `status.go`, `list.go`, `heartbeat.go`, `checkpoint.go`, `release.go`, `transfer.go` and tests. Out of scope: more than one resource per claim (fail invalid-resource with a hint naming TASK-85.8), handles, guarded execution, history and events views.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Lifecycle tests with an injected clock prove acquire returns revision 1 and a 64-hex token whose SHA-256 is the only stored value; heartbeat and checkpoint advance the revision by one and set expiresAt to now plus ttl; release removes the claim, finalizes the epoch with reason released and the retained checkpoint, and appends a released event; transfer creates a successor at revision 1 with the checkpoint carried over, ends the predecessor epoch with reason transferred and successorClaimId, and a contender subprocess never observes the resource free during the transfer.
- [ ] #2 Credential tests prove the check order of contract 7.3 (unknown claim id gives stale-claim, wrong token gives invalid-token, expired claim gives claim-expired, wrong revision gives stale-revision with expectedRevision and suppliedRevision), that none of these failures mutate rows or append events, and that an out-of-bounds ttl or a checkpoint over 8 KiB fails before any write.
- [ ] #3 Idempotency tests prove replaying an operation id with the same request returns the stored receipt with idempotent true and no revision change, a changed request fails operation-request-mismatch, a started operation created through BeginOperation fails unknown-outcome on replay, and an acquire replay with the same claim id and a different ttl fails operation-request-mismatch.
- [ ] #4 Clock tests prove a forward jump expires the claim and the next acquire replaces it with an expired-replaced event and an epoch end effective at the stored expiry, a backward step under 1 s keeps the claim active without changes, a larger regression observed by a contending acquire re-anchors heartbeatAt and expiresAt and appends clock-regression, and `--wait` retries on the injected monotonic clock then fails wait-timeout with holder metadata while `--wait 0` performs one attempt.
- [ ] #5 Two subprocesses acquiring the same resource 50 times each have exactly one winner per round while independent resources proceed concurrently; CLI tests prove every command in text and JSON with explicit credentials, that status and list never contain tokens, that two -r values fail invalid-resource naming TASK-85.8, and `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
