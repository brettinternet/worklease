---
id: TASK-85.6
title: Implement secure authority storage and POSIX locking
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.4
references:
  - src/worklease/sqlite.py
  - src/worklease/locking.py
  - src/worklease/store.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 98000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Build the fresh local authority beneath the resolved Worklease state home. Preserve the important security and concurrency properties of the proof of concept while using an intentionally new Go-owned schema. The storage package should expose narrow transactional operations rather than leaking SQL into CLI or MCP code.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Authority directories and secret-bearing files enforce owner-only permissions, reject symlinks and non-regular files, and use no-follow opens where supported on Linux and macOS.
- [ ] #2 Opaque resources map to deterministic lock files, singleton locks are nonblocking, and bundle locks use a deterministic deadlock-free order across processes.
- [ ] #3 A fresh versioned SQLite schema stores resources, claims, bundles, operations, terminations, checkpoints, and event cursors with transactional integrity.
- [ ] #4 Schema creation and migration are serialized, future or malformed schemas fail safely, and no Python database compatibility is attempted or implied.
- [ ] #5 Unit, multi-process contention, migration, corruption, rollback, permission, and race tests pass with bounded timeouts.
<!-- AC:END -->
