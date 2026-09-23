---
id: TASK-126.3
title: Define no-effect cancellation in the generic workflow contract
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies: []
references:
  - skills/worklease-workflow/references/contract.md
  - skills/worklease-workflow/SKILL.md
  - docs/claim-model.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-126
priority: high
type: docs
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
contract.md requires a verified provider checkpoint before any release. Suppose a human claims an item in the queue and then abandons it without doing any work. The only honest option today is to let the lease expire, and a claim on a source with no progress write can never be released at all. Plan section 8 calls for a narrowly defined cancellation. TASK-130.2 (release) and TASK-132.1 (write pipeline) implement it.

Cancellation must not weaken checkpoint-before-release for real work, and it must not manufacture a completion signal.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 contract.md defines cancellation as a release with a non-completion reason. It is allowed only when no guarded operation was started and no provider write was dispatched under that ownership epoch. Every other release still requires checkpoint-before-release
- [ ] #2 The contract states that cancellation never implies completion, never creates a provider or Worklease checkpoint, and reports its own distinct outcome in the result vocabulary
- [ ] #3 The contract states that an unresolved or unknown operation forbids cancellation
- [ ] #4 skills/worklease-workflow/SKILL.md and doc-1 are consistent with the new rule, with doc-1 edited only through `backlog doc update`
- [ ] #5 The guarantees in docs/claim-model.md are unchanged. Any cross-reference added there points to the contract instead of restating it
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run doc-test` passes and every changed relative link resolves
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
