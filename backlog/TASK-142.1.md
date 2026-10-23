---
id: TASK-142.1
title: Probe Linear API behavior on a dedicated test team
status: done
type: spike
priority: medium
labels: [work-queue, reviewed]
milestone: m-1
parent: TASK-142
rank: a75ab909e175ab8a2
refs: [docs/work-queue-tui-proposal.md]
created: 2026-09-25T16:30:00Z
updated: 2026-09-26T04:41:00Z
---

D29 and the Linear declarations are hypotheses. Use only the user-approved dedicated Linear test team, create synthetic issues for mutations, and clean them up. The user offered an API key for this probe; obtain it securely when beginning, never place it in the task or logs.

## Acceptance criteria

- [x] Record viewer, organization and team IDs; issue UUID versus identifier across team moves; Relay page limits, order, and updatedAt filtering in §3
- [x] Measure whether relation add/remove changes updatedAt on either endpoint; record archive, trash, and permission-loss visibility
- [x] Establish workflow state types, single-assignee semantics, request and complexity limits/headers, Markdown operation-marker round-trip, and native claim absence or capabilities against §9
- [x] Clean up every synthetic issue and revise D29 and §7 for any contradicted assumptions; no existing issue is mutated

## Plan

1. Obtain Linear API key via secure local credential store and confirm a dedicated disposable test team. 2. Probe only synthetic issues with bounded GraphQL reads/mutations, record sanitized identity, pagination, relations, visibility, workflow, limits and marker evidence. 3. Delete/archive every synthetic issue, update proposal §3/D29/§7 with evidence and uncertainties, run repository quality gates, commit and merge the worktree, then finalize the task from the primary checkout.

## Notes

Live TEST/PDEV probe 2026-09-25: viewer 72088203-6bc7-4a63-a71b-22048b88da64, org 0bfebf80-70af-4eca-9e39-2029a01f5b77. Two synthetic issues only: UUID stable across TEST→PDEV→TEST; first<=250, updatedAt.gte and descending Relay cursor work, but updated-order page transition skipped a moved row. blocks add bumped inverse endpoint only; removal bumped both. Archived/trash entries require includeArchived and do not bump updatedAt; permanent deletion confirmed by empty team list. Single assignee field, workflow types, rate/complexity headers and separate comment marker read-back confirmed. Query/Mutation schema exposed no per-issue claim/lease/lock; no native §9 authority admitted. Permission loss intentionally untested: no restricted principal and no approved membership mutation; inaccessible/missing remains unknown (not deletion). Both synthetic issues permanently deleted; no existing issue mutated. Review: one general pass corrected stale table heading and clarified edge timestamp safety; no open item-scoped defect. Checks: git diff --check; mise run lint, format-check, test, typecheck, hooks all passed. Docs commit 5ac3b8f; merge 9372c09 on main. Next: TASK-142.2 credential helper, independent of the probe; TASK-142.3 can now pin vectors. No secret placed in task or repo.

## Summary

Recorded live Linear TEST/PDEV identity, pagination, relation, state, quota, marker and archive/trash evidence in §3/D29/§7; synthetic issues permanently deleted. Merged 5ac3b8f via 9372c09; lint, format-check, tests, typecheck and hooks passed. Permission-loss visibility remains untested and must fail closed.

## Comments

### 2026-09-26T04:41:00Z @pi

Post-completion review of the Linear probe evidence found no additional item-scoped defect; permission-loss visibility remains explicitly unprobed and fails closed. No follow-up required for this completed spike.
