---
id: TASK-142.2
title: Add a bounded generic queue credential helper
status: To Do
assignee: []
created_date: '2026-09-25 16:30'
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
- [ ] #1 queue.yaml accepts a user-configured credential helper argv for approved remote sources without storing credentials
- [ ] #2 Helper execution bounds runtime/output, scrubs environment, redacts stderr, keeps tokens in memory only, and serializes refresh per credential
- [ ] #3 Principal mismatch or helper failure disables authorized writes with actionable diagnostics; tests exercise secret redaction and concurrency
<!-- AC:END -->
