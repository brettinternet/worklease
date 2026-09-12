---
id: TASK-85.6
title: Implement the secure authority store and schema
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 06:27'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.4
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/sqlite.py
  - tests/test_store.py
  - tests/test_history.py
  - ../hum/internal/daemon/runtime.go
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 98000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement the fresh secure SQLite authority store from contract section 8, using the proven driver. Own internal/store schema/open/transaction/event code and required testkit database helpers. No Python migration/import and no per-resource locks.

Create immutable authorityId and durable event/clock watermarks, epoch replay hashes and recorded end time, bounded replay deadlines, resolver claim identity and the unique started-operation constraint. Enforce private main/WAL/SHM paths through the actual driver, not a disconnected preflight open. Read commands do not create or chmod state. Use typed transaction boundaries and classify uncertain commits honestly.

Evidence and patterns (the amended contract is normative): `src/worklease/sqlite.py` (lease_home, secure_directory, open_private_file, schema bootstrap and version checks). Tests: `tests/test_store.py` test_state_home_and_files_are_private_with_a_permissive_umask, test_state_database_symlink_is_rejected, test_unknown_schema_version_fails_before_migration, test_schema_failure_closes_new_connection, test_up_to_date_open_skips_schema_writes; `tests/test_history.py` test_events_rejects_symlinked_state. Pattern: hum `internal/daemon/runtime.go` for runtime-directory permission handling.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Fresh write bootstrap produces private directories/main/WAL/SHM and rejects symlinks, hard links, unsafe ancestors and foreign ownership; tests include adversarial path swaps.
- [ ] #2 Schema creation is atomic across competing processes, includes all amended tables/indexes/watermarks, preserves authorityId across reopen, rejects corrupt/unsupported schemas, and performs no writes on current-schema read opens.
- [ ] #3 Write transactions serialize and roll back on pre-commit failure; read-only access cannot write or create state, and commit-error read-back distinguishes committed/not-committed/unknown.
- [ ] #4 AppendEvent atomically updates last_event_seq and validates a typed allowlist of public fields; tests prove cross-process order, no visibility before commit and no private nested payloads.
- [ ] #5 Constraint tests prove one started operation per claim and stored epoch authentication/retention metadata; mise run ci-go passes with no per-resource authority lock files.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
