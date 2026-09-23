---
id: TASK-132.3
title: Add GitHub Issues write operations
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - github
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
- [ ] #1 Close and reopen send a state reason and are verified by read-back of state and stateReason. A `not_planned` close is never presented as successful completion
- [ ] #2 Start, blocked, and review intents are unavailable with reason `no-workflow-mapping` unless a supported mapping exists per the plan section 17 decision
- [ ] #3 Record progress posts a comment containing the HTML-comment marker and is verified with the TASK-132.1 rules
- [ ] #4 Assign to me uses the add-assignees endpoint, never removes other assignees, and is verified by read-back
- [ ] #5 Body and checklist edits are unavailable with reason `no-conditional-body-write`
- [ ] #6 The principal is verified before every write and after credential changes. On a mismatch, writes are refused and no request is sent
- [ ] #7 Mutations are spaced at least 1 s apart per account, and rate-limit headers are honored. A write whose outcome is uncertain is never retried
- [ ] #8 Both plan section 17 GitHub questions are answered in the plan before completion, and unattended writes stay disabled until the headless-identity decision is recorded
- [ ] #9 Tests against the fake GitHub cover each operation, principal mismatch, lost responses with lagging read-back, and rate limiting
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
