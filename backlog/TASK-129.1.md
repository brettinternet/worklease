---
id: TASK-129.1
title: Add the disposable queue index with single-flight refresh
status: done
type: feature
priority: high
assignees: ['@brett']
labels: [work-queue, reviewed]
milestone: m-1
parent: TASK-129
blocked_by: [TASK-128]
rank: a25b9efd4e25b9edc
refs: [internal/store/schema.go, internal/store, docs/work-queue-tui-proposal.md]
created: 2026-09-23T04:29:00Z
updated: 2026-09-23T23:56:00Z
---

D19 puts a disposable per-user SQLite cache under the user cache directory. It is separate from the authority database and from the S6 recovery journal, and partitioned by source, principal, and configuration generation. All queue processes share it, and one advisory file lock per partition ensures a single refresher. Other processes read the index and wait for, or report, the in-flight refresh. This lets agents call `queue query` in loops without multiplying provider traffic. It also lets the TUI render a cached first view with no network dependency (plan section 14, first budget row).

Reuse the repository's existing SQLite driver and migration style (internal/store) where it fits, but keep the index in its own file and package. Nothing in the index is ever required for correctness: deleting it is always safe.

## Acceptance criteria

- [x] The index lives at `$XDG_CACHE_HOME/worklease/queue/` (falling back to ~/.cache) in WAL mode with owner-only permissions and a schema generation. An unknown or corrupt index is rebuilt automatically, and deleting it never loses unresolved data
- [x] Rows are partitioned by source id, principal and access scope, and configuration generation. A query never returns rows from another partition, and changing the account, scopes, or configuration invalidates the affected partitions
- [x] When revalidation shows an item is no longer accessible, it is purged from every derived projection: summaries, FTS, bodies, dependency graphs, and counts. Cached payloads have an explicit retention bound, as tested by revoking access for the same account
- [x] An advisory file lock enforces one refresher per partition across processes. A test with several concurrent processes shows exactly one provider refresh while the others wait or report `refresh-in-progress`
- [x] `queue query --max-age DURATION` serves the index when it is fresh enough and otherwise joins or starts the single-flight refresh. The JSON reports the observation time and whether it was served from the index
- [x] An FTS table indexes summary fields, and every search result states its coverage (loaded rows, indexed summaries, or indexed bodies). Body indexing is opt-in
- [x] A row is removed only after a completed scan generation or explicit deletion evidence from the adapter. Rows missing from an incomplete scan are kept
- [x] The TUI renders a first view from the index before any network request completes

## Definition of done

- [x] `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit

## Plan

1. Build a disposable, permission-partitioned SQLite queue index with schema rebuild, retention, FTS and scan-safe reconciliation. 2. Add cross-process single-flight refresh and wire --max-age into CLI and cached-first TUI. 3. Test isolation, revocation, incomplete scans, concurrent processes and refresh parity; run gates, review, commit and integrate.

## Notes

Index core under implementation in task-129.1-index worktree; CLI/TUI integration, revocation and concurrency acceptance checks remain. Worklease claim held by current loop.

Implemented owner-private WAL index, scoped cache identity, complete/incomplete reconciliation, explicit revocation, FTS and body opt-in, cross-process lock, CLI max-age and TUI cached first frame. Independent review found four concrete defects (checkout replacement, lock order, stale fallback, empty cache); corrected and retested. Verified mise run lint, format-check, test, typecheck, hooks, focused cache and first-frame tests after rebase; code commits 6a528bc and 3586e31 fast-forwarded into main. GitHub persistence bypassed without provable access scope; unsupported OS bypasses cache.

Found during TASK-128.x review: concurrent Open raced schema creation, misread it as corruption and rebuilt (deleted) a live index. Fixed with BEGIN IMMEDIATE migration (b33180f, merged 7268f9a). Not a full review of this task.

Review: lock contention no longer rebuilds (deletes) a live index; concurrent queries reuse a refresh completed while waiting; cached TUI frame no longer waits on remote metadata; Backlog cache identity bound to branch/HEAD; retention invalidates partition freshness; corrupt gen-2 schema rebuilt; body-search coverage honest; adapter-to-index revocation test (4751120). Tests had leaked hook GIT_DIR into git fixtures and corrupted the repo (core.bare=true, stray commits); IsolateProcessEnvironment now strips GIT_*, queue/queueindex/queueui/instructions get isolation TestMain, lefthook unsets GIT_DIR/GIT_WORK_TREE. GitHub cache-identity finding covered by TASK-129.3's sync lock and live-authorization design. Merged to main 013a058.

## Summary

Added disposable queue index and cached-first CLI/TUI; verified full gates, cross-process lock, revocation, scope isolation, empty-cache reuse, stale fallback and first-frame rendering. Integrated 6a528bc and 3586e31 into main.
