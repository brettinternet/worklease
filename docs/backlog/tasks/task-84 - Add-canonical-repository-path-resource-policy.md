---
id: TASK-84
title: Add canonical repository path resource policy
status: Done
assignee: []
created_date: '2026-09-12 03:06'
updated_date: '2026-09-12 04:45'
labels: []
dependencies: []
references:
  - 'https://github.com/dicklesworthstone/mcp_agent_mail'
  - docs/claim-model.md
priority: low
type: feature
ordinal: 91000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Task-level claims do not prevent two different tasks from modifying the same file or subsystem. Callers can invent generic resource strings, but without one canonical repository-path policy, agents in linked worktrees can derive different keys for the same logical path and fail to contend. A built-in policy should support explicit path or subsystem intent without introducing task tracking or advisory mailbox state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The policy derives a deterministic resource for an explicitly selected repository-relative file or directory and reports its exact local coordination capability and scope.
- [ ] #2 The same logical repository path derives the same resource across linked worktrees, while paths in different repositories do not collide.
- [ ] #3 The policy rejects traversal, repository escape, ambiguous repository identity, and unsupported path forms instead of guessing.
- [ ] #4 Exact path claims have documented semantics; parent/child and arbitrary glob overlap are not implied unless explicitly supported and tested.
- [ ] #5 Documentation shows atomically bundling an authoritative task resource with one or more path resources and preserves the external provider as task authority.
- [ ] #6 Tests cover files, directories, linked worktrees, separate repositories, invalid paths, stable key derivation, bundles, and guarantee language.
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Closed 2026-09-12 as superseded before starting the unattended go-rewrite loop, so readiness-based selection cannot pick Python-era work. Acceptance criteria intentionally left unchecked: they were not delivered here. Delivery is owned by TASK-85.5.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Superseded by TASK-85.5 in the Go rewrite (TASK-85). Not delivered in Python. Go Product Contract (docs/backlog/docs/go-rewrite/doc-2) section 7.13 (path policy keyed by git common dir and repo-relative path) fixes the behavior.
<!-- SECTION:FINAL_SUMMARY:END -->
