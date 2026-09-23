---
id: TASK-126.3
title: Define no-effect cancellation in the generic workflow contract
status: Done
assignee:
  - '@executor'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:49'
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
- [x] #1 contract.md defines cancellation as a release with a non-completion reason. It is allowed only when no guarded operation was started and no provider write was dispatched under that ownership epoch. Every other release still requires checkpoint-before-release
- [x] #2 The contract states that cancellation never implies completion, never creates a provider or Worklease checkpoint, and reports its own distinct outcome in the result vocabulary
- [x] #3 The contract states that an unresolved or unknown operation forbids cancellation
- [x] #4 skills/worklease-workflow/SKILL.md and doc-1 are consistent with the new rule, with doc-1 edited only through `backlog doc update`
- [x] #5 The guarantees in docs/claim-model.md are unchanged. Any cross-reference added there points to the contract instead of restating it
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run doc-test` passes and every changed relative link resolves
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Amend the generic workflow release invariant with narrow non-completion cancellation, no-effect eligibility, and a distinct outcome; unresolved/unknown work forbids cancellation. 2. Align the skill and doc-1 wording using backlog doc update; leave claim-model guarantees unchanged. 3. Verify plan consistency, links, and doc-test, then record evidence and finalize via Backlog CLI.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Added no-effect cancellation as an explicit non-completion release in contract.md, with a distinct cancelled outcome and strict prohibition when any guarded operation/provider write started or outcome is unknown. Aligned SKILL.md and doc-1 using backlog doc update; claim-model.md is unchanged. §8 already states the same cancellation direction, so no proposal edit was required. `mise run doc-test`, `backlog doc view doc-1 --plain`, and `git diff --check` passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Defined cancellation as an eligible no-effect release without checkpoint or completion, forbidding it after any started or uncertain operation. Aligned the skill and doc-1, preserved claim-model guarantees, and verified documentation tests and links.
<!-- SECTION:FINAL_SUMMARY:END -->
