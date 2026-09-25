---
id: TASK-132.2
title: Add Backlog.md write operations
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 15:45'
labels:
  - work-queue
  - backlog-md
  - reviewed
milestone: m-1
dependencies:
  - TASK-132.1
references:
  - skills/worklease-workflow/references/source-providers/backlog-md.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-132
priority: high
type: feature
ordinal: 38000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The plan section 8 operation table enables these Backlog.md writes through documented `backlog task edit` flags only. State changes use `--status` with a configured status, reached through the TASK-132.1 intent mapping. Progress uses `--append-notes` or `--comment` with a trailing `worklease-op:` marker line. Assign to me is a read-modify-write with `--assignee`, which replaces the whole list; that is a declared race, and an assignee added by another writer between the read and the write can be lost.

Checking an acceptance criterion stays read-only: `--check-ac N` targets a mutable index, and pre/post reads can detect but not prevent checking the wrong criterion after a reorder.

Plan section 5 and TASK-126.6 cover Git effects. With auto_commit on, a write creates a commit and may run hooks, and the preview must show that. If TASK-126.6 found that auto-commit captures unrelated staged changes, writes in that mode stay disabled unless the TASK-126.6 findings recorded a safe condition.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Intent-mapped state changes accept only statuses configured in the project and are verified by read-back through `task view --json`
- [x] #2 Record progress appends a note or comment ending in the operation's marker line and is verified with the TASK-132.1 rules
- [x] #3 Assign to me writes the pre-read assignees plus me, and the preview discloses the race. If read-back shows an assignee from the pre-read is missing, or another writer's change is visible, the result is reported as a conflict, never as success, as tested with concurrent assignment edits
- [x] #4 Check criterion is unavailable with reason `unstable-criterion-target`, and no `--check-ac` call is ever sent
- [x] #5 When auto_commit is enabled, the preview states that a commit will be created and whether hooks run. Writes are disabled when the TASK-126.6 findings make auto-commit unsafe with staged changes
- [x] #6 All writes go through the TASK-132.1 pipeline and use argv without a shell. Nothing edits task Markdown files directly
- [x] #7 Integration tests in a scratch project cover each operation, marker verification, assignment conflict, and auto_commit on and off
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a Backlog.md write adapter over the existing recoverable pipeline: validate configured status and exact action payload, derive previews and Git effects from live configuration, and keep criterion-index mutation unavailable. 2. Verify state, append provenance, and assignment read-back against fresh task JSON; classify assignment races as conflicts. 3. Exercise writes and recovery with scratch Backlog projects, including concurrent assignee changes and auto-commit effects; run focused race tests and repository gates. 4. Review, commit, merge to main, record acceptance evidence and release claim.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented isolated Backlog.md write adapter and scratch integration tests for state, progress marker/recovery, assignment conflict, criterion rejection, and auto-commit/hooks/staged isolation. Focused race tests (3x), lint, format-check, full test, typecheck and staged hooks passed in task-132-2-backlog-writes. One general review pass pending; then commit/merge/finalize.

Review: one general pass found hook-preview drift, unsafe comma-bearing assignee, comment prefix spoofing, later Git commit, and later note append; all five were fixed and directly retested. Final focused race go test -race -count=3 -run ^TestBacklogWrite ./internal/queue passed; mise run lint, format-check, test, typecheck, staged hooks passed after fixes. Code/docs commit 7daccf457c969942ead9bfcce55062c3c5adbaa4 fast-forward merged to main; owned worktree and branch removed, Herdr workspace wN8 closed. AC evidence: TestBacklogWritePipelineScratchProject (state status view, notes/comment marker, assignment); TestBacklogWriteAssignmentConflictAndCriterionUnavailable (concurrent edit, no criterion mutation); TestBacklogWriteMarkerAndStatusVerification, LostAppendResponseRecoversWithoutRedispatch, AppendRecoveryWithFollowingWrites, PreviewRejectsHookPolicyDriftAndUnsafeAssignee, CommitVerifiedAfterUnrelatedCommit, AutoCommitPreviewAndReceipt (on/off, hooks, unrelated staged file). Proposal §6 updated in same commit.

Review 2026-09-25 (c5a29ab): 'queue recovery retry' built an unresolved Backlog.md/GitHub adapter, so every cross-process read-back failed with invalid-source. It now resolves the journaled source from queue.yaml, rejects drift, and keeps the held claim visible when the source is unavailable (TestQueueRecoveryAdapterResolvesJournaledBacklogSource).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added recoverable Backlog.md state, progress, and assignment writes with conflict and Git-effect verification. Scratch integration, focused race x3, lint, format, full tests, typecheck, and staged hooks passed; review findings fixed. Merged 7daccf4 to main and removed the worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
