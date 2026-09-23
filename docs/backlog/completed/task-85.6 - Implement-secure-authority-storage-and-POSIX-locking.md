---
id: TASK-85.6
title: Implement the secure authority store and schema
status: Done
assignee:
  - '@pi-01a094a4'
created_date: '2026-09-12 03:22'
updated_date: '2026-09-13 00:20'
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
modified_files:
  - internal/store/driver.go
  - internal/store/driver_identity_darwin.go
  - internal/store/driver_identity_linux.go
  - internal/store/event.go
  - internal/store/schema.go
  - internal/store/store.go
  - internal/store/store_test.go
  - internal/testkit/database.go
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
- [x] #1 Fresh write bootstrap produces private directories/main/WAL/SHM and rejects symlinks, hard links, unsafe ancestors and foreign ownership; tests include adversarial path swaps.
- [x] #2 Schema creation is atomic across competing processes, includes all amended tables/indexes/watermarks, preserves authorityId across reopen, rejects corrupt/unsupported schemas, and performs no writes on current-schema read opens.
- [x] #3 Write transactions serialize and roll back on pre-commit failure; read-only access cannot write or create state, and commit-error read-back distinguishes committed/not-committed/unknown.
- [x] #4 AppendEvent atomically updates last_event_seq and validates a typed allowlist of public fields; tests prove cross-process order, no visibility before commit and no private nested payloads.
- [x] #5 Constraint tests prove one started operation per claim and stored epoch authentication/retention metadata; mise run ci-go passes with no per-resource authority lock files.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add internal/store authority Open/Close with secure owner-private home validation, pinned home directory descriptor, safe database identity checks around the selected modernc driver, and read-only empty-state behavior without filesystem mutation. 2. Add schema version 1 bootstrap in one immediate transaction with the normative meta, claims, resource, epoch, operation, reconciliation, and events tables/indexes, immutable random authority ID, watermarks, and corrupt/unsupported schema validation. 3. Add typed Store/Tx Read and Write boundaries plus commit classification passthrough and a validated Event.AppendEvent implementation that atomically advances last_event_seq and rejects private/unknown payload fields. 4. Extend internal/testkit database helpers and add comprehensive store tests for filesystem adversaries, schema concurrency/identity/corruption/read fast paths, transaction rollback/read-only behavior, event atomicity/order/validation, and operation constraints. 5. Run focused store tests and mise run ci-go, record implementation notes and objective evidence without committing.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed with Worklease for implementation in an isolated Herdr worktree. Guarantee: local coordination among cooperating callers on this host; providerMutationFenced=false.

Implemented internal/store Store.Open with secure owner-private home validation, pinned directory descriptor, read-only empty-state handling, and lstat identity verification around modernc path opens. Added normative schema v1 bootstrap/validation with immutable authority ID, durable watermarks, epochs, operations (one-started partial index), reconciliations, and events. Added typed Store/Tx transactions and Event.AppendEvent allowlist/atomic watermark updates; added internal/testkit database path helpers and store acceptance tests for concurrency, rollback, read-only behavior, path adversaries, event visibility/redaction, and constraints. Focused tests and mise run ci-go pass; no commit made.

Follow-up hardening: schema bootstrap rechecks user_version inside BEGIN IMMEDIATE so competing opens converge on one authority ID; Store writes persist monotonic last_observed_at watermark and read validates canceled contexts; trusted root-owned macOS /var compatibility ancestor is allowed while user-owned/writable ancestors fail. Validation: go test ./internal/store ./internal/testkit, go test -race ./internal/store ./internal/testkit, and mise run ci-go all passed.

Independent adversarial review found path TOCTOU, handles-directory, bootstrap interleaving, schema validation, transaction escape-hatch, and commit read-back gaps. Addressed them with descriptor-based chmod and identity checks, live SQLite descriptor verification on Darwin/Linux, private handles validation, in-transaction empty-schema checks, constraint/watermark validation, package-private raw write access, and Store.WriteWithReadback. Added adversarial final-open swap, home swap, handles, weakened-index/watermark, and cross-process event-order tests. Final validation passed: mise run lint, format-check, test (339 Python tests), typecheck, hooks, ci-go, and Linux CGO-free cross-build.

Implementation commit: 4884501 (Implement secure Go authority store).
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @C3
created: 2026-09-13 00:20
---
Correction after TASK-88: read-only authority access does not create the home or main database, but opening an existing WAL database in a writable directory may update shared-memory coordination state and recreate absent owner-private -wal/-shm sidecars. The amended Go Product Contract sections 8 and 13 are authoritative for AC #3 and the plan/notes wording.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented the secure Go SQLite authority store and schema. AC1: TestOpenCreatesAndValidatesPrivateHandlesDirectory, TestHomeSwapCannotChmodSymlinkTargetAndRootIsRejected, TestStoreRejectsFinalSQLiteOpenIntervalSwap, and existing driver unsafe-path tests prove private state and adversarial rejection. AC2: TestOpenBootstrapsNormativeSchemaAndStableAuthority, TestCompetingWriteOpensBootstrapOneSchema, TestSchemaRejectsUnsupportedAndCorruptWithoutMigration, and TestSchemaRejectsWeakenedStartedOperationIndexAndWatermark prove atomic bootstrap and schema integrity. AC3: TestWriteRollbackReadDeferredAndAppendEventBoundary plus TestCommitErrorOutcomeUsesFreshDurableReadback prove transaction and commit classification behavior. AC4: TestAppendEventRejectsPrivateAndUnknownNestedPayload, TestAppendEventCrossConnectionInvisibleBeforeCommit, and TestAppendEventCrossProcessSequenceOrder prove event validation, atomicity, and order. AC5: TestOperationStartedConstraintAndEpochMetadata and schema tests prove operation/epoch constraints. Final evidence: mise run lint, format-check, test, typecheck, hooks, and ci-go all pass; Linux CGO-free cross-build passes.
<!-- SECTION:FINAL_SUMMARY:END -->
