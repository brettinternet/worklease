---
id: TASK-83
title: Add Worklease environment diagnostics
status: Done
assignee: []
created_date: '2026-09-12 03:06'
updated_date: '2026-09-12 04:45'
labels: []
dependencies: []
references:
  - docs/mcp.md
  - docs/claim-model.md
priority: medium
type: enhancement
ordinal: 90000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Lease failures caused by separate authority homes, unsafe permissions, missing MCP extras, absent agent identity, linked-worktree assumptions, or unsuitable host clock state are difficult to diagnose from individual lifecycle commands. A read-only diagnostic command should identify common integration mistakes without acquiring claims or exposing secrets.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A read-only diagnostic command reports the resolved authority home, state and handle permission checks, contextual repository/worktree identity, agent identity source, MCP availability, and relevant clock assumptions.
- [ ] #2 Each check has a stable machine-readable identifier and status, with actionable text guidance for warnings and failures.
- [ ] #3 Diagnostics distinguish verified facts from conditions that cannot be established and do not claim that separate hosts or provider writes are fenced.
- [ ] #4 The command does not create state, acquire or mutate claims, read secret-bearing columns, or emit bearer credentials and lease-file contents.
- [ ] #5 Tests cover healthy, missing, insecure, split-authority, linked-worktree, unavailable-component, and redaction cases.
- [ ] #6 The MCP and agent setup documentation uses the diagnostic command as the supported verification step.
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Closed 2026-09-12 as superseded before starting the unattended go-rewrite loop, so readiness-based selection cannot pick Python-era work. Acceptance criteria intentionally left unchecked: they were not delivered here. Delivery is owned by TASK-85.14.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Superseded by TASK-85.14 in the Go rewrite (TASK-85). Not delivered in Python. Go Product Contract (docs/backlog/docs/go-rewrite/doc-2) section 13 (read-only doctor with stable check ids) fixes the behavior.
<!-- SECTION:FINAL_SUMMARY:END -->
