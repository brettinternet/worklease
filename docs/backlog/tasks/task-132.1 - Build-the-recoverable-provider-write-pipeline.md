---
id: TASK-132.1
title: Build the recoverable provider write pipeline
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 15:45'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies:
  - TASK-130
references:
  - internal/lease/service.go
  - internal/guard/guard.go
  - internal/handle/handle.go
  - skills/worklease-workflow/references/contract.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-132
priority: high
type: feature
ordinal: 37000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan section 8 ("Every write has a recoverable boundary") defines five steps:
1. Check capability, caller authority, scope, and the current claim; refresh the item and its closure; preserve unrelated fields.
2. Persist the exact intent in an owner-private recovery journal before dispatch.
3. Dispatch a minimal, coordination-only write.
4. Obtain the receipt and independently read back the intended fields.
5. Only then record any Worklease checkpoint and resolve any guarded operation.

Append-only writes carry a `worklease-op:<operationID>` marker. It locates a candidate result after a lost response, but it is a correlation aid, not proof (D7). Verification also checks the exact item, the journaled payload, available receipt provenance, and every declared side effect. The marker never makes a retry safe.

The authority's guarded-operation API supports only `exec` and `replace-file` operations (internal/lease/service.go). Use it only for effects it genuinely covers, never label a direct HTTPS mutation as `exec`, and add no new operation kind. The queue journal stores provider-side intent and receipts. It lives apart from the disposable TASK-129.1 index and survives logout and cache eviction.

This task also defines the provider-neutral workflow intents (start, blocked, review, complete, reopen) and their per-source mapping in queue.yaml, plus action-specific eligibility from TASK-126.7. It is tested with the fake adapter; TASK-132.2 and TASK-132.3 plug in the real providers.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The write adapter interface returns a provider receipt, and readReceipt returns verified, conflict, or unknown. The pipeline runs steps 1-5 in order and refuses to dispatch when any step 1 check fails
- [x] #2 Intent (operation ID, source, principal, patch, precondition, and claim and operation references) is fsynced to an owner-private journal outside the cache directory before dispatch. Records are retained for a bounded period after resolution and never removed while unresolved, including on logout or cache eviction
- [x] #3 A lost-response result counts as verified only when the marker is found exactly once on the intended source and item, the appended content matches the journal, receipt identity and actor match where the provider exposes them, and every declared side effect is confirmed. A copied marker, changed content, duplicate matches, or an unverifiable side effect stays unresolved, as tested for each case
- [x] #4 When no matching result is visible, the outcome stays unknown. The write is never re-dispatched under the same or a new ID, and it stays in recovery until verification succeeds or an operator records reconciliation evidence
- [x] #5 Guarded operations are used only for effects the authority supports (`exec`, `replace-file`). A direct API write runs through the journaled coordination-only path, and its lost-response recovery is tested without any guarded-operation receipt
- [x] #6 queue.yaml accepts a per-source `workflow` map from intents (start, blocked, review, complete, reopen) to provider transitions, validated by the adapter. Unmapped intents are unavailable with reason `no-workflow-mapping`, and no status is ever invented
- [x] #7 Eligibility is action-specific, following TASK-126.7: start requires readiness; a verified current owner can report Blocked or record progress when prerequisites are unsatisfied, without becoming start-eligible; complete requires its declared evidence
- [x] #8 When acquisition succeeded but a pre-dispatch check fails, the result is reported as "claim held / source unchanged" and a safe release is offered. When the write succeeded but checkpointing failed, the receipt is kept and recovery finishes without repeating the write
- [x] #9 The TASK-126.3 cancellation is refused as soon as any intent has been journaled or any guarded operation begun under the claim
- [x] #10 Fault-injection tests cover a crash or error before persisting the intent, after persisting it, after dispatch, after the receipt, during read-back (including lagging read-back), and during the checkpoint, and assert no duplicate dispatch in every case
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add explicit per-source workflow intent mappings with adapter validation, and action-specific eligibility tests.
2. Introduce a provider-neutral write adapter and durable owner-private intent/receipt journal outside the disposable index.
3. Implement pre-dispatch revalidation, single dispatch, read-back verification, recovery and checkpoint/cancellation guards; test fake-adapter failure boundaries and marker provenance.
4. Run focused race tests and repository quality gates, review, commit in isolated worktree, merge to main, and finalize verified criteria.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented provider-neutral recoverable write path in 668b07b and transition rejection test in 1db89cc, both fast-forward merged to main. Fake adapter fault tests cover preflight refusal, durable journal/privacy/retention, marker content/provenance and effects, unknown response/recovery, operator reconciliation, action-specific eligibility, exact checkpoint replay after lost response, and cancellation-vs-write interleaving. Verified focused race tests (3 runs) for queue/config/cli, plus mise run lint, format-check, test, typecheck, staged hooks. One general review found two concrete races (cancellation admission and committed checkpoint response loss); both fixed and rerun with focused tests. No proposal decision was contradicted or refined; §8 is unchanged. No remaining blocker. Next: TASK-132.2 and TASK-132.3 can implement concrete provider write adapters against this boundary; not started here.

Review 2026-09-25 (c5a29ab): recovery of a checkpoint-pending record re-read the provider first, so a committed checkpoint could not resolve while the provider was unavailable; now finishes the checkpoint directly (TestWritePipelineCrashAfterCommittedCheckpoint with read error). Follow-up TASK-138: a verified write whose checkpoint misses CheckpointNotAfter has no terminal path and blocks later writes on the item. Backlog.md append provenance rests on marker+content since the provider exposes no independent author; accepted per AC #3.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added durable provider-write intent journal, verified read-back/recovery, exact checkpoint replay, safe cancellation coordination, and explicit workflow mappings; tested fault boundaries and race interleavings. Merged 668b07b and 1db89cc to main; all required gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
