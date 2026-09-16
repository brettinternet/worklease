---
id: DRAFT-2
title: Support remote file replacement
status: Draft
assignee: []
created_date: '2026-09-16 06:03'
labels:
  - remote-authority
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Remote profiles currently reject replace-file because the authority is remote while the filesystem effect is client-local. Define a safe client-executed replacement protocol that preserves claim ownership, exact pending recovery, bounded execution, local path validation, and the existing rule that the server never executes filesystem effects.
<!-- SECTION:DESCRIPTION:END -->
