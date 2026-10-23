---
id: TASK-157
title: Report symlinked ancestors accurately in owner-private path checks
status: todo
rank: ab57213c2eb57209c
created: 2026-10-23T22:39:00Z
updated: 2026-10-23T22:39:00Z
---

On macOS, /tmp and /var are symlinks to /private/... . `worklease server init` (and enroll/invite artifact writes) under such a path fails with "output directory must be owner-private" even when the directory itself is 0700 and owned by the caller. The real cause is the symlinked ancestor, so operators chase permissions that are already correct. Found while building the bl remote-authority test (backlog BL-72), which had to EvalSymlinks every home.

## Acceptance criteria

- [ ] When an owner-private check fails because an ancestor is a symlink, the error names the symlinked component and suggests the resolved path
- [ ] The permission-mode error is reported only when the mode or owner is actually wrong
- [ ] Tests cover a 0700 directory beneath a symlinked ancestor
