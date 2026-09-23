---
id: TASK-126.4
title: Add queue identity fixture vectors for claim resources
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 20:59'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies: []
references:
  - internal/resource/resource.go
  - internal/resource/resource_test.go
  - internal/cli/resource_commands.go
  - internal/cli/resource_commands_test.go
  - internal/lease/remote.go
  - internal/testkit/git.go
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-126
priority: high
type: task
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Queue claims and CLI or agent claims exclude each other only if every contender derives byte-identical resources (plan section 6, D12, D24). Pin the expected bytes as shared fixtures before any queue code exists. The resource tests then enforce them now, and the queue tests in TASK-128.6, TASK-128.7, and TASK-130.1 reuse them later.

This task adds fixtures and tests only. It must not change KeyPolicyVersion, the bytes of any existing key, or policy behavior. If a vector reveals surprising existing behavior, record it in plan section 6 rather than changing the policy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A versioned fixture file under internal/resource/testdata/ maps explicit inputs to exact resources for these cases: backlog-md for the same relative source directory from a primary checkout and from a linked worktree (identical keys), and for the repository root versus a nested source directory (distinct keys, because the locator includes the repo-relative path); a generic portable binding, where a declared source name plus a task ID yields `coordination:generic:<sha256>`; github owner/repo, including case and `.git` normalization; github host/owner/repo for an enterprise host; and markdown source-wide scope
- [x] #2 Tests assert that `worklease key --json` output equals each vector byte for byte
- [x] #3 The default backlog-md key and the generic portable key for the same task differ, documenting that a worker on the default policy does not contend with a portable binding even on the same authority (D12)
- [x] #4 Negative vectors show that a URL-form GitHub locator yields a different key than owner/repo, and that blank or invalid inputs are rejected
- [x] #5 An admission test shows that generic vectors are admitted under the default `coordination:` remote prefix, that github vectors are rejected unless `github:` is admitted, and that host-local backlog-md vectors are always rejected remotely
- [x] #6 KeyPolicyVersion and all pre-existing key outputs are unchanged
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a versioned JSON fixture catalog with explicit inputs and exact expected resource strings; represent the physical Git common directory with a documented placeholder substituted only by tests. 2. Reuse the catalog in resource tests and exercise every vector through worklease key --json, including portability and negative cases. 3. Verify remote prefix admission against catalog resources and confirm policy version and established outputs remain unchanged. 4. Run focused and required project gates; record evidence through the Backlog CLI.

5. User approved the minimal production-output correction exposed by the vectors: preserve locally policy-derived coordination digests in key JSON without relaxing redaction of raw resource/source/item strings. KeyPolicyVersion, derivation bytes, and policy behavior remain unchanged.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Scope narrowed by user to TASK-126.4 only. Prior stage exposed generic key JSON digest redaction and root locator "."; retain current key semantics, correct fixtures, document both observations, and include the specifically approved output fix. Commit only TASK-126.4 deliverables; preserve unrelated task and proposal changes.

Verified 10 positive and 5 invalid shared vectors: resource policy and CLI JSON parity; linked-worktree equality, root/nested distinction, generic/default separation, GitHub normalization/URL distinction, Markdown source-wide identity, and default/expanded remote admission. Existing static-policy golden tests pass; internal/resource/resource.go is unchanged and KeyPolicyVersion remains 1. Redaction regressions verify locally computed generic/linear hashes survive while caller resource/source/item strings and secret fields remain redacted. Focused tests passed, followed by mise run lint, format-check, test, typecheck, hooks-install, and staged hooks. One scoped inspection found no remaining defects; no optional polish or additional review pass. Plan section 6 records the root locator and approved output-only fix. Only this task, its fixture/tests/output fix, and its section 6 paragraph are included in the delivery commit; unrelated work remains uncommitted.

Review: queue default backlog-md keys used the checkout root, diverging from the CLI's backlog-directory source; ClaimSources now resolves the backlog directory (tested).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added versioned queue identity vectors and resource/CLI/admission coverage. Fixed the approved computed-digest JSON redaction defect without changing policy v1 or derived key bytes. All acceptance criteria and required quality gates passed. Delivered together with this task record in commit: Add TASK-126.4 identity vectors. No remaining blocker.
<!-- SECTION:FINAL_SUMMARY:END -->
