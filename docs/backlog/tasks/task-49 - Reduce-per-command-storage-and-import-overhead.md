---
id: TASK-49
title: Reduce per-command storage and import overhead
status: To Do
assignee: []
created_date: '2026-09-07 03:28'
labels:
  - performance
dependencies: []
references:
  - src/worklease/sqlite.py
  - src/worklease/store.py
  - src/worklease/execution.py
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
- [ ] #1 Opening an up-to-date database performs no CREATE, ALTER, or UPDATE statements; a test asserts the migration path runs only on version mismatch or first creation
- [ ] #2 acquire uses a single connection; exec reuses one connection for its lifetime and no longer stores the bearer token in operation receipts
- [ ] #3 status and list read within one deferred transaction; list issues a bounded number of queries independent of claim count
- [ ] #4 policy list scans entry points once; importing worklease.cli no longer imports tempfile or shutil
- [ ] #5 A before/after timing table for --version, status, acquire, heartbeat, and list at 0 and 5000 claims is recorded in the task
<!-- AC:END -->
