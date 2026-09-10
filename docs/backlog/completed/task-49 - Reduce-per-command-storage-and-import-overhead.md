---
id: TASK-49
title: Reduce per-command storage and import overhead
status: Done
assignee:
  - '@codex-task-49'
created_date: '2026-09-07 03:28'
updated_date: '2026-09-07 14:13'
labels:
  - performance
dependencies: []
references:
  - src/worklease/sqlite.py
  - src/worklease/store.py
  - src/worklease/execution.py
modified_files:
  - src/worklease/__init__.py
  - src/worklease/adapters/registry.py
  - src/worklease/cli.py
  - src/worklease/execution.py
  - src/worklease/sqlite.py
  - src/worklease/store.py
  - tests/test_adapters.py
  - tests/test_cli.py
  - tests/test_execution.py
  - tests/test_store.py
priority: medium
type: enhancement
ordinal: 50000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Profiling (2026-09-06) measured about 80 ms per command: about 20 ms interpreter, about 47 ms required stdlib imports, and the rest in worklease. Storage work is small today but scales badly.

Findings to address:
- `sqlite.connect()` runs the full schema script on every open: blocking global flock, about 10 CREATE TABLE IF NOT EXISTS, PRAGMA table_info probes, 9 CREATE INDEX IF NOT EXISTS, and an unconditional reconciliations backfill UPDATE (target_claim_id empty) that is O(rows). Measured 1.0 ms empty, 4.9 ms at 50k reconciliations. Gate the script on a schema_meta version check and run backfills once.
- `LeaseStore.acquire()` opens two connections per call (preflight bundle check plus the locked transaction).
- `exec` renewals persist one operations row per internal heartbeat (a one-hour exec writes about 720 rows) and reopen a connection each time; those rows also store the receipt with the token (`_advance_claim` uses include_token=True while complete_operation deliberately does not).
- `status` and `list` issue multiple statements outside a transaction and can mix snapshots with a concurrent release_bundle; `list_claims` is 1+2N queries.
- `policy list` calls importlib.metadata.entry_points() once per policy (6x).
- `worklease/__init__.py` eagerly imports `.replacement` (tempfile, shutil, compression) and `.execution` although only exec and replace-file need them (about 10 ms).
- `PRAGMA synchronous = FULL` under WAL; decide whether NORMAL is acceptable for the same-host durability promise and document the decision either way.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Opening an up-to-date database performs no CREATE, ALTER, or UPDATE statements; a test asserts the migration path runs only on version mismatch or first creation
- [x] #2 acquire uses a single connection; exec reuses one connection for its lifetime and no longer stores the bearer token in operation receipts
- [x] #3 status and list read within one deferred transaction; list issues a bounded number of queries independent of claim count
- [x] #4 policy list scans entry points once; importing worklease.cli no longer imports tempfile or shutil
- [x] #5 A before/after timing table for --version, status, acquire, heartbeat, and list at 0 and 5000 claims is recorded in the task
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a schema-version fast path so current databases avoid setup DDL/DML, while first creation and version mismatches run locked migrations once; retain FULL synchronous durability unless evidence supports weakening it.
2. Reuse SQLite connections for acquire and guarded-exec renewals, omit bearer tokens and internal heartbeat operation rows, and make status/list transactional with set-based bundle projection queries.
3. Scan policy entry points once per listing operation and lazily expose execution/replacement exports so importing worklease.cli avoids tempfile and shutil.
4. Add focused regression/query-count/import tests, benchmark required commands before and after at 0 and 5000 claims, then run all quality gates and review the diff.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented schema fast-path/migrations, single-connection acquire and guarded execution, non-ledger internal renewals with redacted persisted receipts, transactional bounded-query projections, one-pass policy discovery, and lazy package exports. Retained `PRAGMA synchronous = FULL` because committed lease transitions require same-host crash durability.

Benchmark: median wall-clock milliseconds across 15 fresh CLI subprocess samples per command; each fixture used a temporary database seeded with the stated active-claim count.

| Claims | Command | Before (ms) | After (ms) |
|---:|---|---:|---:|
| 0 | `--version` | 73.0 | 73.4 |
| 0 | `status` | 67.4 | 63.8 |
| 0 | `acquire` | 70.3 | 66.7 |
| 0 | `heartbeat` | 68.0 | 66.4 |
| 0 | `list` | 67.4 | 64.7 |
| 5000 | `--version` | 74.5 | 73.5 |
| 5000 | `status` | 69.6 | 64.3 |
| 5000 | `acquire` | 72.2 | 66.8 |
| 5000 | `heartbeat` | 71.6 | 65.5 |
| 5000 | `list` | 153.5 | 126.9 |

Validation passed after review fix: `mise run lint`, `mise run format-check`, `mise run test` (219 core + 19 SDK tests), `mise run typecheck`, and staged-file `mise run hooks`. Review found interrupted migration recovery was not resumable; fixed by allowing an empty schema marker to migrate and added `test_empty_schema_marker_resumes_interrupted_migration`. No staged files remained after hook validation.

Post-delivery review (TASK-56) found and fixed defects in this work; see TASK-56 for the specific defect, the fix, and its regression test.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Reduced command overhead with schema-version fast paths, connection reuse, token-safe internal exec renewals, transactional bounded-query reads, one-pass policy discovery, and lazy heavy imports. Added regression coverage and recorded 0/5000-claim benchmarks; all project quality gates and hooks pass after addressing review findings.
<!-- SECTION:FINAL_SUMMARY:END -->
