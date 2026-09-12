---
id: TASK-91
title: Bound replace-file time inside the authority write transaction
status: To Do
assignee:
  - '@brett'
created_date: '2026-09-12 22:39'
labels:
  - go-rewrite
dependencies: []
references:
  - internal/guard/guard.go
  - internal/lease/service.go
priority: medium
type: enhancement
ordinal: 116000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
guard.ReplaceFile runs its whole effect inside RunGuardedOperation's BEGIN IMMEDIATE transaction: it reads the entire content file, writes and fsyncs the temp file, then renames. That is the authority's only write lock, so a large replacement stalls every other writer on the host until SQLite's 10 second busy timeout returns storage-failure to them. The contract wants the expected-hash check and rename serialized with ownership transitions, but not the bulk I/O. Reading and digesting the content before the transaction, or capping the accepted content size, keeps the serialized section to the hash compare, temp write, and rename.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Content is read and digested before the completing write transaction, and the transaction re-verifies the content digest rather than re-reading the file
- [ ] #2 A documented maximum replacement size is enforced with invalid-argument before any started intent is committed
- [ ] #3 A test shows a concurrent heartbeat completes while a replace-file of the maximum size is in progress
<!-- AC:END -->
