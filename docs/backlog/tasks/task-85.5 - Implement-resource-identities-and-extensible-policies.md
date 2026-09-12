---
id: TASK-85.5
title: Implement resource identities and extensible policies
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.2
  - TASK-85.3
references:
  - src/worklease/adapters
  - docs/source-provider-sdk-compatibility.md
  - ../hum/internal/config/config.go
  - TASK-76
  - TASK-84
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Port the useful provider-neutral resource identity behavior without carrying forward Python entry-point loading or the unused source-provider SDK. Keep built-in backlog, GitHub, Linear, Markdown, generic, and repository-path use cases, and define a small Go-native extension seam only where it simplifies adding deterministic policies. This task absorbs the useful intent of TASK-76 and TASK-84.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Built-in policies derive deterministic resource identities for backlog, GitHub, Linear, Markdown, generic, and explicit repository-relative path inputs with truthful scope and fencing capability metadata.
- [ ] #2 The same repository and logical path resolve consistently across linked worktrees, distinct repositories do not collide, and traversal or ambiguous paths are rejected.
- [ ] #3 The policy boundary is a documented Go interface with conformance tests and registration that does not require dynamic Go plugins, Python, provider credentials, or network access.
- [ ] #4 The CLI can derive and acquire a resource in one invocation while retaining a standalone key inspection command and clear validation for conflicting input modes.
- [ ] #5 Tests cover normalization, stable hashing, linked worktrees, invalid identities, capability language, duplicate registrations, and every built-in policy.
<!-- AC:END -->
