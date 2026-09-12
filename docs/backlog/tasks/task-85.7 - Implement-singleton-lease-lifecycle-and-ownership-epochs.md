---
id: TASK-85.7
title: Implement the claim lifecycle service
status: In Progress
assignee:
  - '@pi-01a094d6'
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 10:11'
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
Implement the claim lifecycle service and initial lifecycle CLI against the amended contract sections 4, 6–8, 18 and 20. Own internal/lease and acquire/status/list/heartbeat/checkpoint/release/transfer commands. This slice supports exactly one resource using the shared resource-list model; 85.8 lifts the limit.

The authority accepts client-held credentials and never returns tokens. Until 85.10 adds pending handles, expose the stateless path with caller-retained identity/credential/request inputs. Implement acquire replay, epoch-authenticated lifecycle replay, exact normalized request hashes including maxDuration and replay deadline, contention/wait, conservative clock handling, checkpoint recovery, atomic transfer/release, and started-operation primitives. One mutation has one operation identity, while a guard's start/renew/complete transitions share its operation row.

The service carries authority identity and typed domain results; it does not accept CLI paths, remote transports, or provider writes. Keep all lifecycle events transactional and make unresolved predecessor operations visible to later guard/reconciliation consumers.

Provide authenticated operation lookup and current-state synchronization for 85.10 pending-handle recovery; 85.9 adds user-facing ledger projections and reconciliation on top.

Evidence and patterns (the amended contract is normative): `src/worklease/claims.py`, `acquisition.py` (singleton path; acquire replay keyed on the claim id), `lifecycle.py` (transfer and release), `operations.py` (ledger, request fingerprint, replay, mismatch, unknown outcome), `models.py` (bounds). Tests in `tests/test_store.py`: test_expiry_reclaim_replaces_token_and_increases_revision, test_small_backward_clock_step_keeps_a_live_lease, test_backward_clock_regression_is_reanchored_by_a_contender (the Go contract fails closed instead), test_forward_clock_step_past_expiry_still_expires, test_checkpoint_renews_replays_and_rejects_stale_owner, test_transfer_replaces_owner_atomically_and_preserves_checkpoint, test_transfer_rolls_back_on_interruption_without_free_interval, test_transfer_serializes_contender_acquire, test_acquire_retry_rejects_any_ttl_change, test_stale_owner_cannot_heartbeat_after_reclaim, test_heartbeat_retry_is_idempotent_and_rejects_request_mismatch, test_release_retry_is_idempotent_and_claim_id_cannot_be_reused, test_claim_survives_process_exit_and_can_be_reclaimed, test_invalid_ttl_and_blank_release_reason_do_not_change_state. Tests in `tests/test_cli.py`: test_wait_retries_transient_contention_until_acquire, test_no_wait_remains_one_atomic_attempt, test_wait_timeout_preserves_conflict_exit_code_and_redaction, test_heartbeat_distinguishes_invalid_token_from_stale_claim, test_acquire_defaults_generate_and_echo_identifiers, test_lifecycle_redacts_read_only_tokens_and_supports_text_list.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Lifecycle tests prove atomic acquire/renew/checkpoint/release/transfer and epoch end/checkpoint recovery, client-held 64-hex credentials with hashes only in authority state, successor revision 1, release audit reason and no transfer free interval.
- [ ] #2 Authorization tests prove ordered current-claim/token/expiry/revision checks without failed-write side effects, exclusive explicit selection, bounds validation and token-free public status/list.
- [ ] #3 Replay tests authenticate original epochs after release/transfer, return recorded receipts without reviving ownership, reject changed TTL/maxDuration/cwd/content intent, enforce requestNotAfter and preserve current revisions; no replay returns a token.
- [ ] #4 Clock/wait tests cover forward expiry, bounded small rollback clamping, larger regression failing closed without re-anchoring, monotonic jittered wait deadlines and redacted contention metadata.
- [ ] #5 Cross-process contention has exactly one winner; started-operation tests reject overlapping guarded starts and unrelated lifecycle mutations, permit only guard-internal renewal/completion, and expose predecessor unknowns after expiry; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add typed internal/lease models and a SQLite-backed singleton lifecycle service with clock validation, credential hashing, exact request hashing/replay, atomic epoch transitions, and started-operation primitives.
2. Wire acquire/status/list/heartbeat/checkpoint/release/transfer through the existing CLI boundary using stateless credentials until TASK-85.10 adds handles, preserving redaction and bounded monotonic wait behavior.
3. Add focused service, replay, clock, contention, started-operation, and CLI tests; append the Unreleased changelog entry.
4. Run mise run ci-go, independently review acceptance evidence, fix findings, then finalize the Backlog task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed with Worklease for isolated implementation; local coordination scope only, provider mutations are not fenced.

Implemented the singleton Go claim lifecycle in an isolated worktree, including authenticated replay, conservative clocks, atomic epoch transitions, stateless CLI wiring, predecessor recovery, and guarded-operation primitives. Independent review found and drove fixes for replay ordering, credential selection, wait bounds, redaction, and cross-process evidence. mise run ci-go passes.
<!-- SECTION:NOTES:END -->
