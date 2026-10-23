---
id: TASK-161
title: Support remote file replacement
status: draft
labels: [remote-authority]
rank: af8d0649a7f8d056e
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

Remote profiles currently reject replace-file because the authority is remote while the filesystem effect is client-local. Define a safe client-executed replacement protocol that preserves claim ownership, exact pending recovery, bounded execution, local path validation, and the existing rule that the server never executes filesystem effects.
