---
id: TASK-138
title: Give expired queue write checkpoints a terminal recovery path
status: Done
assignee: []
created_date: '2026-09-25 15:45'
updated_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies: []
references:
  - internal/queue/write.go
  - internal/cli/queue_recovery.go
documentation:
  - docs/work-queue-tui-proposal.md
priority: medium
type: bug
ordinal: 60000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Found reviewing TASK-132.1. When a provider write is verified but its Worklease checkpoint did not commit before intent.CheckpointNotAfter (for example a lost checkpoint response followed by a laptop sleep past the default one-hour deadline, or read-back that first succeeds after the deadline), WritePipeline.finishCheckpoint returns 'checkpoint replay deadline expired; reconciliation required' (internal/queue/write.go). But Reconcile only accepts status 'unknown' with no receipt or verified read-back, so it refuses this record. The record stays unresolved forever, is never pruned, and WritePipeline.Start refuses every later queue write on that item ('item has unresolved provider write'). The only exit today is hand-editing the owner-private journal. The fix needs a design decision: an operator-attested 'provider verified, checkpoint missing' outcome (distinct from no-commit reconciliation, and never replaying an expired checkpoint request), or an automatic terminal outcome once the authority proves the checkpoint can no longer commit.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A verified provider write whose checkpoint can no longer commit reaches a terminal journal status that is distinct from verified and from no-commit reconciliation
- [x] #2 Reaching that status never re-dispatches the provider write and never replays an expired checkpoint request
- [x] #3 After it, a new queue write on the same item is no longer refused by the unresolved-record guard
- [x] #4 The CLI 'queue recovery' commands and the TUI Recovery view expose the path, and docs/queue.md describes it
- [x] #5 A fake-adapter test advances the clock past CheckpointNotAfter with a verified provider effect and no committed checkpoint and exercises the path
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Use operator-attested provider-verified/checkpoint-missing terminal status: require expired request, original authority reporting no committed checkpoint, executor cessation and typed evidence; never replay after expiry. Preserve verified provider evidence even when claim expires.
2. Expose the attestation in CLI and TUI Recovery view; keep terminal journal admission/retention and docs consistent.
3. Test fake clock, expired claim, authority ambiguity/commit; run focused and repository gates, review corrections, commit, merge and finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Initial automatic closure failed review: client clock and unknown authority status cannot prove absence of in-flight commit; changing to explicit operator attestation. Full test gate exposed intermittent pre-existing external adapter stderr diagnostic failure; isolated rerun passed; checking full gate after correction.

Evidence: TestWritePipelineExpiredCheckpointRecovery verifies expired clock, verified provider receipt and lost append with expired claim, authority outage/committed checkpoint refusal, no redispatch/replay, terminal journal removal from Recovery, uncancellable effectful claim, and fresh item dispatch. TestRecoveryCheckpointMissingAttestationAndTerminalNotice exercises TUI attestation and terminal notice. CLI help and zero-flag audit verify the checkpoint-missing command. Merged implementation 6554ef2 into main; post-merge mise run lint, format-check, test, typecheck and focused race count=3 all passed. Single general review found unsafe automatic closure, expired claim guard and TUI stale warning; all addressed by operator attestation, reordering claim verification, and terminal UI handling. Initial full test run intermittently failed in unrelated external adapter stderr diagnostic, isolated rerun and later full suites passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Operator-attested checkpoint-missing recovery closes verified provider writes after expired checkpoints without redispatch or replay; CLI/TUI/docs and fake-adapter regression added. Verified on main by full lint, format, test, typecheck and focused race checks; implementation merged as 6554ef2.
<!-- SECTION:FINAL_SUMMARY:END -->
