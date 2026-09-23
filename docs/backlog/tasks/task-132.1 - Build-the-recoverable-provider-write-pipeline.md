---
id: TASK-132.1
title: Build the recoverable provider write pipeline
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
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
- [ ] #1 The write adapter interface returns a provider receipt, and readReceipt returns verified, conflict, or unknown. The pipeline runs steps 1-5 in order and refuses to dispatch when any step 1 check fails
- [ ] #2 Intent (operation ID, source, principal, patch, precondition, and claim and operation references) is fsynced to an owner-private journal outside the cache directory before dispatch. Records are retained for a bounded period after resolution and never removed while unresolved, including on logout or cache eviction
- [ ] #3 A lost-response result counts as verified only when the marker is found exactly once on the intended source and item, the appended content matches the journal, receipt identity and actor match where the provider exposes them, and every declared side effect is confirmed. A copied marker, changed content, duplicate matches, or an unverifiable side effect stays unresolved, as tested for each case
- [ ] #4 When no matching result is visible, the outcome stays unknown. The write is never re-dispatched under the same or a new ID, and it stays in recovery until verification succeeds or an operator records reconciliation evidence
- [ ] #5 Guarded operations are used only for effects the authority supports (`exec`, `replace-file`). A direct API write runs through the journaled coordination-only path, and its lost-response recovery is tested without any guarded-operation receipt
- [ ] #6 queue.yaml accepts a per-source `workflow` map from intents (start, blocked, review, complete, reopen) to provider transitions, validated by the adapter. Unmapped intents are unavailable with reason `no-workflow-mapping`, and no status is ever invented
- [ ] #7 Eligibility is action-specific, following TASK-126.7: start requires readiness; a verified current owner can report Blocked or record progress when prerequisites are unsatisfied, without becoming start-eligible; complete requires its declared evidence
- [ ] #8 When acquisition succeeded but a pre-dispatch check fails, the result is reported as "claim held / source unchanged" and a safe release is offered. When the write succeeded but checkpointing failed, the receipt is kept and recovery finishes without repeating the write
- [ ] #9 The TASK-126.3 cancellation is refused as soon as any intent has been journaled or any guarded operation begun under the claim
- [ ] #10 Fault-injection tests cover a crash or error before persisting the intent, after persisting it, after dispatch, after the receipt, during read-back (including lagging read-back), and during the checkpoint, and assert no duplicate dispatch in every case
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
