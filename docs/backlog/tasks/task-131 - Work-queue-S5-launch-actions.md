---
id: TASK-131
title: 'Work queue S5: launch actions'
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 15:10'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-131.1
  - TASK-131.2
documentation:
  - docs/work-queue-tui-proposal.md
priority: high
type: feature
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Users want to start an agent or script on a selected item from the queue. D18 and plan section 12 define launch actions: user-configured argv templates with item references, run in an allowlisted environment that never carries credentials or item content. The launched worker acquires its own claim. The queue neither supervises nor retries it. See plan sections 10, 12, 15, and 16 (S5).

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every S5 child task is Done
- [x] #2 With secret variables set in the queue's environment, none reach the child unless named in `passEnv`
- [x] #3 Hostile titles and option-like IDs cannot inject arguments
- [x] #4 Claim then Launch, and launch with an unresolved cwd, are refused with explanations
- [x] #5 A tested launcher consumes the handoff, and the worker verifies the same authority and exact resources, including for portable Backlog.md bindings
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reverify S5 child delivery and focused launcher, gate, environment and portable-binding acceptance tests in an isolated worktree on main. 2. Run repository quality gates, record evidence and review outcome with Backlog CLI in primary checkout and commit task finalization there (the worktree has no source delta to merge). 3. Verify provider state and release the claim.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
S5 children TASK-131.1 and TASK-131.2 are Done on main (00de99f, 4050c17, 23e9f0c). Isolated worktree focused runs passed: TestLaunchExecHandoffIsolatedEnvironment (explicit GH_TOKEN/passEnv, unlisted GITHUB_TOKEN/CANARY_SECRET excluded, hostile title/body and option-like item ID isolated as argv), TestLaunchGatesAndPublicPreview, TestPrepareLaunchUnresolvedAndInvalidIdentity (queue-owned claim and unavailable cwd refused), TestLaunchHandoffIncludesRetiredBindingKeys, TestReferenceLauncherClaimsExactQueueHandoff (GitHub and portable Backlog generic binding, same authority and exact resources, mismatch refused), TestQueueQueryReportsEachLaunchActionAndItsGate and queue picker/overlay tests. Full mise run lint, format-check, test, typecheck passed. One general integration review of launch handoff and tests found no item-scoped defects. No source change required; only provider task finalization pending commit.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed S5 integration checklist on main: both children Done; verified env isolation, injection resistance, launch gates and portable Backlog/GitHub worker handoff with focused integration tests and full lint/format/test/typecheck. No source changes required.
<!-- SECTION:FINAL_SUMMARY:END -->
