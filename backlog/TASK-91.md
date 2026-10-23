---
id: TASK-91
title: Bound replace-file time inside the authority write transaction
status: done
type: enhancement
priority: medium
assignees: ['@pi-01a097fc']
labels: [go-rewrite]
rank: ac59d31674c59d268
refs: [internal/guard/guard.go, internal/lease/service.go]
created: 2026-09-12T22:39:00Z
updated: 2026-09-12T23:55:00Z
---

guard.ReplaceFile runs its whole effect inside RunGuardedOperation's BEGIN IMMEDIATE transaction: it reads the entire content file, writes and fsyncs the temp file, then renames. That is the authority's only write lock, so a large replacement stalls every other writer on the host until SQLite's 10 second busy timeout returns storage-failure to them. The contract wants the expected-hash check and rename serialized with ownership transitions, but not the bulk I/O. Reading and digesting the content before the transaction, or capping the accepted content size, keeps the serialized section to the hash compare, temp write, and rename.

## Acceptance criteria

- [x] Content is read and digested before the completing write transaction, and the transaction re-verifies the content digest rather than re-reading the file
- [x] A documented maximum replacement size is enforced with invalid-argument before any started intent is committed
- [x] A test shows a concurrent heartbeat completes while a replace-file of the maximum size is in progress

## Plan

1. Trace replace-file preparation and transaction boundaries, then define a bounded preflight representation and maximum size.
2. Move content reading and digest verification before RunGuardedOperation while retaining in-transaction target hash verification and atomic rename.
3. Add invalid-size and concurrent-heartbeat regression tests, document the limit, and run focused plus repository gates.

## Notes

Implemented a 16 MiB replacement-content limit and pinned, bounded preflight read before lifecycle preparation. The completing transaction hashes the prepared byte snapshot again before temp-file write and rename.

Evidence: TestReplaceFileRejectsOversizedContentBeforeStarting proves invalid-argument, no lifecycle preparation, unchanged revision, and no unknown operation. TestReplaceFilePreflightDoesNotBlockConcurrentHeartbeat pauses a maximum-size preflight while a different claim heartbeats through the same authority. go test -race ./internal/guard and the repository lint, format-check, test, and typecheck gates pass. Direct item-scoped diff review found no defects; the independent reviewer attempt timed out without returning findings.

Delivery: implementation commit d523cb4; merged to main as c5fa89d. Post-merge focused guard tests passed.

## Summary

Bounded replace-file input to 16 MiB and moved its pinned read/digest outside the authority write transaction while retaining digest verification, expected-target checks, atomic rename, and completion inside the serialized boundary. Added oversize/no-intent and concurrent-heartbeat regression coverage and documented the limit.
