---
id: TASK-85.6
title: Implement the secure authority store and schema
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.3
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
The Go authority needs a fresh, transactional, owner-only SQLite store with the schema and the event primitive every later task builds on. The Python proof of concept proved which safety checks matter (owner-only directories, symlink rejection, no-follow opens, WAL with FULL sync, serialized schema creation) but mixed them with a three-version migration history and per-resource flock files. The contract drops the lock files (D8) and starts a new schema (section 8). This task delivers exactly that store.

Read first: contract sections 2 (D8, D9, D14), 3, 8, and 18 (store sketch), plus the driver amendment TASK-85.4 recorded in contract section 17. Python evidence: `src/worklease/sqlite.py` (lease_home, secure_directory, open_private_file, schema bootstrap and version checks), and in `tests/test_store.py`: test_state_home_and_files_are_private_with_a_permissive_umask, test_state_database_symlink_is_rejected, test_unknown_schema_version_fails_before_migration, test_schema_failure_closes_new_connection, test_up_to_date_open_skips_schema_writes; `tests/test_history.py` test_events_rejects_symlinked_state. Pattern: `../hum/internal/daemon/runtime.go` for runtime-directory permission handling.

Deliver in `internal/store`: `Open(ctx, home, Options)` performing the section 8 safety checks (create the 0700 home and `handles/` with umask 0077, chmod when owned, fail `home-unsafe` on a foreign owner or symlink, lstat the database path, `O_NOFOLLOW|O_CLOEXEC` opens), then `OpenDriver` from TASK-85.4, then `user_version` handling (0 creates version 1 in one BEGIN IMMEDIATE; 1 verifies the tables; anything else fails `schema-unsupported`; missing tables fail `schema-corrupt`); `Write` and `Read` transaction helpers; `Tx.AppendEvent` as the only event writer, validating the kind against contract 7.12 and rejecting `Detail` keys named token, tokenHash, checkpoint, evidence, stdout, or stderr; typed row helpers for claims, claim_resources, epochs, epoch_resources, operations, reconciliations, events, and meta (get and set pruned_through_seq); UnixMicro time helpers; `Options{ReadOnly}` opening `mode=ro` without creating anything (for doctor and read commands). Extend `internal/testkit` `OpenReadOnly` to work against the real store.

Owned paths: `internal/store` (except `driver.go`), `internal/testkit/db.go`. Out of scope: claim semantics, the lease service, CLI commands, garbage collection logic.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Permission tests prove a fresh home is created 0700 with handles/ at 0700 and the database at 0600 under umask 0022; a symlinked home, a symlinked database path, and a foreign-owned home (injected stat) fail home-unsafe (75); a group-readable home owned by the caller is chmodded to 0700 and opening succeeds.
- [ ] #2 Schema tests prove opening an empty database creates every table and index from contract section 8 and sets user_version 1 in one transaction, reopening performs no schema writes (asserted through a statement counter or unchanged file mtime), user_version 2 fails schema-unsupported, user_version 1 with a dropped table fails schema-corrupt, and two subprocesses opening the same empty home concurrently both succeed with exactly one schema creation.
- [ ] #3 Transaction tests prove Write uses BEGIN IMMEDIATE and rolls back completely when fn returns an error or the context is cancelled, Read cannot write, and Options{ReadOnly} never creates a home or a database.
- [ ] #4 AppendEvent tests prove seq increments monotonically across processes, unknown kinds are rejected, forbidden Detail keys are rejected, and an event row is visible to another connection only after commit.
- [ ] #5 `mise run ci-go` passes including -race for internal/store, and the package contains no per-resource lock file code.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
