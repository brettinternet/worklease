---
id: TASK-126.3
title: Define no-effect cancellation in the generic workflow contract
status: done
type: docs
priority: high
assignees: ['@executor']
labels: [work-queue, reviewed]
milestone: m-1
parent: TASK-126
rank: a072f9b658072f9b0
refs: [skills/worklease-workflow/references/contract.md, skills/worklease-workflow/SKILL.md, docs/claim-model.md, docs/work-queue-tui-proposal.md]
created: 2026-09-23T04:29:00Z
updated: 2026-09-23T20:59:00Z
---

contract.md requires a verified provider checkpoint before any release. Suppose a human claims an item in the queue and then abandons it without doing any work. The only honest option today is to let the lease expire, and a claim on a source with no progress write can never be released at all. Plan section 8 calls for a narrowly defined cancellation. TASK-130.2 (release) and TASK-132.1 (write pipeline) implement it.

Cancellation must not weaken checkpoint-before-release for real work, and it must not manufacture a completion signal.

## Acceptance criteria

- [x] contract.md defines cancellation as a release with a non-completion reason. It is allowed only when no guarded operation was started and no provider write was dispatched under that ownership epoch. Every other release still requires checkpoint-before-release
- [x] The contract states that cancellation never implies completion, never creates a provider or Worklease checkpoint, and reports its own distinct outcome in the result vocabulary
- [x] The contract states that an unresolved or unknown operation forbids cancellation
- [x] skills/worklease-workflow/SKILL.md and doc-1 are consistent with the new rule, with doc-1 edited only through `backlog doc update`
- [x] The guarantees in docs/claim-model.md are unchanged. Any cross-reference added there points to the contract instead of restating it

## Definition of done

- [x] `mise run doc-test` passes and every changed relative link resolves
- [x] Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit

## Plan

1. Amend the generic workflow release invariant with narrow non-completion cancellation, no-effect eligibility, and a distinct outcome; unresolved/unknown work forbids cancellation. 2. Align the skill and doc-1 wording using backlog doc update; leave claim-model guarantees unchanged. 3. Verify plan consistency, links, and doc-test, then record evidence and finalize via Backlog CLI.

## Notes

Added no-effect cancellation as an explicit non-completion release in contract.md, with a distinct cancelled outcome and strict prohibition when any guarded operation/provider write started or outcome is unknown. Aligned SKILL.md and doc-1 using backlog doc update; claim-model.md is unchanged. §8 already states the same cancellation direction, so no proposal edit was required. `mise run doc-test`, `backlog doc view doc-1 --plain`, and `git diff --check` passed.

Review: source-workflow.md now permits the contract's no-effect cancellation instead of forbidding all release on unsupported pre-dispatch writes.

## Summary

Defined cancellation as an eligible no-effect release without checkpoint or completion, forbidding it after any started or uncertain operation. Aligned the skill and doc-1, preserved claim-model guarantees, and verified documentation tests and links.
