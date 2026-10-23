---
id: TASK-157
title: Report symlinked ancestors accurately in owner-private path checks
status: To Do
assignee: []
created_date: '2026-10-23 22:39'
labels: []
dependencies: []
ordinal: 100000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
On macOS, /tmp and /var are symlinks to /private/... . `worklease server init` (and enroll/invite artifact writes) under such a path fails with "output directory must be owner-private" even when the directory itself is 0700 and owned by the caller. The real cause is the symlinked ancestor, so operators chase permissions that are already correct. Found while building the bl remote-authority test (backlog BL-72), which had to EvalSymlinks every home.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 When an owner-private check fails because an ancestor is a symlink, the error names the symlinked component and suggests the resolved path
- [ ] #2 The permission-mode error is reported only when the mode or owner is actually wrong
- [ ] #3 Tests cover a 0700 directory beneath a symlinked ancestor
<!-- AC:END -->
