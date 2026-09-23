---
id: TASK-126.4
title: Add queue identity fixture vectors for claim resources
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
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
- [ ] #1 A versioned fixture file under internal/resource/testdata/ maps explicit inputs to exact resources for these cases: backlog-md for the same relative source directory from a primary checkout and from a linked worktree (identical keys), and for the repository root versus a nested source directory (distinct keys, because the locator includes the repo-relative path); a generic portable binding, where a declared source name plus a task ID yields `coordination:generic:<sha256>`; github owner/repo, including case and `.git` normalization; github host/owner/repo for an enterprise host; and markdown source-wide scope
- [ ] #2 Tests assert that `worklease key --json` output equals each vector byte for byte
- [ ] #3 The default backlog-md key and the generic portable key for the same task differ, documenting that a worker on the default policy does not contend with a portable binding even on the same authority (D12)
- [ ] #4 Negative vectors show that a URL-form GitHub locator yields a different key than owner/repo, and that blank or invalid inputs are rejected
- [ ] #5 An admission test shows that generic vectors are admitted under the default `coordination:` remote prefix, that github vectors are rejected unless `github:` is admitted, and that host-local backlog-md vectors are always rejected remotely
- [ ] #6 KeyPolicyVersion and all pre-existing key outputs are unchanged
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
