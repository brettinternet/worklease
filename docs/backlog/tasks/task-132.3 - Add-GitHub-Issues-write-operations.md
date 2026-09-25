---
id: TASK-132.3
title: Add GitHub Issues write operations
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 15:45'
labels:
  - work-queue
  - github
  - reviewed
milestone: m-1
dependencies:
  - TASK-132.1
references:
  - skills/worklease-workflow/references/source-providers/github-issues.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-132
priority: high
type: feature
ordinal: 39000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The plan section 8 operation table enables these GitHub writes: close or reopen with a state reason, record progress as an issue comment carrying an HTML-comment `worklease-op:` marker, and assign to me through the add-assignees endpoint, which preserves other assignees. Body task lists stay disabled, because editing the body replaces it whole with no compare-and-set. No GitHub unsafe method is conditional, so every write is coordination-only. GitHub Issues has no In Progress, Blocked, or review state without a configured, supported Projects or label mapping, and the queue must not invent one.

Plan section 10 requires verifying `viewer.login` against the configured account before any write and after every credential change. Mutations are spaced at least 1 s apart per account through the TASK-129.2 scheduler. Two plan section 17 questions must be answered in the plan before this task completes: whether to map GitHub Projects v2 status (deferred by default, per section 7), and which headless identity unattended writes use. Until the second is decided, writes are interactive only.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Close and reopen send a state reason and are verified by read-back of state and stateReason. A `not_planned` close is never presented as successful completion
- [x] #2 Start, blocked, and review intents are unavailable with reason `no-workflow-mapping` unless a supported mapping exists per the plan section 17 decision
- [x] #3 Record progress posts a comment containing the HTML-comment marker and is verified with the TASK-132.1 rules
- [x] #4 Assign to me uses the add-assignees endpoint, never removes other assignees, and is verified by read-back
- [x] #5 Body and checklist edits are unavailable with reason `no-conditional-body-write`
- [x] #6 The principal is verified before every write and after credential changes. On a mismatch, writes are refused and no request is sent
- [x] #7 Mutations are spaced at least 1 s apart per account, and rate-limit headers are honored. A write whose outcome is uncertain is never retried
- [x] #8 Both plan section 17 GitHub questions are answered in the plan before completion, and unattended writes stay disabled until the headless-identity decision is recorded
- [x] #9 Tests against the fake GitHub cover each operation, principal mismatch, lost responses with lagging read-back, and rate limiting
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Defer Projects v2 status mapping; use explicit GitHub App installation identity for future unattended writes, leaving unattended writes disabled until configured and verified. 2. Implement interactive GitHub issue state, comment, and add-assignees writes through the recoverable pipeline with fresh principal checks and shared account rate scheduling. 3. Exercise operations, lost responses, and rate limits against the fake API; run repository gates, review, commit, integrate, and finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
d1d1201 fast-forward merged to main. Fake GitHub tests verify close/reopen reasons and NOT_PLANNED refusal; unmapped intents/body edits; comment marker/author and lagging lost-response recovery without redispatch; add-assignees preserves others; credential rotation/principal mismatch refuses mutation; rate-limited write honors Retry-After and is not retried. Scheduler fake-clock test verifies >=1 s account mutation slots. Focused race test: go test -race -count=3 -run TestGitHub(Write|NotPlanned|Lost|Writes) ./internal/queue passed. mise run lint, format-check, test, typecheck, hooks and commit hook passed. One general review surfaced mutation spacing, lagging state/assignment read-back, and read-only recovery gaps; all corrected and rerun, final reviewer PASS. Initial suite attempt hit known queueindex TestLockIsSingleFlightAcrossProcesses flake (TASK-136.1); subsequent full suite and hooks passed. Decision: defer Projects v2; choose future explicit GitHub App installation identity; unattended writes remain disabled. Worktree task-132-3-github-writes removed with branch after merge; no remaining blocker.

Review 2026-09-25 (c5a29ab): same CLI recovery resolution fix as TASK-132.2 applies to GitHub. Issue summaries now keep a non-success close reason in rawStatus (CLOSED:NOT_PLANNED) beside the terminal category (TestGitHubSummaryDisclosesNonSuccessCloseReason).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added interactive GitHub issue state, progress-comment, and add-assignee writes with principal verification, rate scheduling, and recoverable read-back; tested fake provider and race paths, all required gates passed. Merged d1d1201 to main and removed worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
