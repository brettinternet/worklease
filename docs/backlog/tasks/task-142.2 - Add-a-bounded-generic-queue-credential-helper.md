---
id: TASK-142.2
title: Add a bounded generic queue credential helper
status: Done
assignee: []
created_date: '2026-09-25 16:30'
updated_date: '2026-09-25 17:34'
labels:
  - work-queue
milestone: m-1
dependencies: []
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: feature
ordinal: 66000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Linear and Jira Cloud need user-configured source credentials without embedding tokens in queue.yaml or inheriting secrets into child processes. Keep GitHub gh helper behavior unchanged. Design the helper so future providers can reuse it without adding auth frameworks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 queue.yaml accepts a user-configured credential helper argv for approved remote sources without storing credentials
- [x] #2 Helper execution bounds runtime/output, scrubs environment, redacts stderr, keeps tokens in memory only, and serializes refresh per credential
- [x] #3 Principal mismatch or helper failure disables authorized writes with actionable diagnostics; tests exercise secret redaction and concurrency
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a validated helper argv field to the approved remote-source configuration without changing gh auth.
2. Implement a bounded, scrubbed, per-credential serialized token resolver with safe diagnostics and principal verification gate for future adapters.
3. Cover failure, redaction, and concurrency with focused tests; run project gates, commit, merge and finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged code commit 4a7add4 via 9c84d53. Config test validates Linear/Jira Cloud helper argv and rejects missing/invalid commands; GitHub auth is unchanged. Helper tests exercise principal mismatch/failure redaction, bounded stdout/stderr, scrubbed child env, timeout/process-group teardown and serialized refresh. Focused -race -count=3 on config and queue passed on main; mise run lint, format-check, test, typecheck and staged hooks passed in implementation worktree. One review found process-group and stderr-limit defects; both fixed and retested. No Linear/Jira adapter is shipped in this subtask, so claims and provider writes remain disabled until later subtasks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added a bounded, principal-verified queue credential helper and Linear/Jira Cloud helper-argv configuration; merged 9c84d53. Focused race tests and all project gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
