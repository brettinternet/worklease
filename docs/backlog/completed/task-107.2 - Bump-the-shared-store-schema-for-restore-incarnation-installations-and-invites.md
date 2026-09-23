---
id: TASK-107.2
title: >-
  Bump the shared store schema for restore incarnation, installations, and
  invites
status: Done
assignee:
  - '@brett'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 02:36'
labels:
  - remote-authority
dependencies:
  - TASK-107.1
references:
  - internal/store/schema.go
  - internal/store/store.go
  - internal/doctor
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 134000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Bump the shared SQLite store to schema version 2 for the remote invariants while keeping `internal/store` storage-only. The one-way migration applies to existing local homes as well as hosted homes. It adds restore incarnation and recovery state, installation and invite authentication records, reopening attestations, persisted per-claim admission limits, trusted installation attribution, remote provenance, bootstrap invite state and replay, and the retained replay, cursor, and recovery references required by the frozen protocol. No HTTP, role policy, or authentication decision belongs in this task.

The restore identifier is cryptographically random and compared only for equality. Migration must preserve populated v1 state, including active claims and started operations, and must be atomic under concurrent openers and injected failures. Old binaries continue to reject the newer schema with their existing unsupported-schema behavior; the new binary reports supported version details where its error contract allows them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `store.SchemaVersion` is 2, and opening a v1 home migrates it once in one `BEGIN IMMEDIATE` transaction. Concurrent openers produce one valid v2 store, and injected failures roll back without a partial schema.
- [x] #2 Migration tests cover empty and populated v1 homes, including active claims, started operations, replay rows, checkpoints, cursors, and retained history, plus already-v2 opens and concurrent migration.
- [x] #3 `meta` stores a cryptographically random `restore_id` created at bootstrap or migration and preserved across ordinary reopen; `doctor` reports it beside `authorityId` without exposing a secret.
- [x] #4 The schema supports `restored` and `revoked` epoch end reasons; per-claim admitted TTL and absolute hold limits; trusted installation attribution separate from agent labels; retained remote and incarnation provenance; recovery mode; bootstrap invite flags, hashes, and replay; and reopening attestations with selected backup cutoff, lost-history interval, unknown bounds, coverage gaps, and private evidence references.
- [x] #5 `verifySchema` requires the v2 tables and constraints, and the existing public event validation accepts the `restored` and `revoked` end reasons without weakening prior validation.
- [x] #6 Installation and invite tables store immutable identifiers, roles, non-unique labels, hashes rather than plaintext secrets, issuer and request provenance, revocation and expiry state, and bounded one-time redemption replay with database constraints enforcing valid states.
- [x] #7 `internal/store` exposes only storage primitives and contains no HTTP types, bearer processing, authorization policy, or server configuration policy. Existing local behavior and tests remain valid apart from the schema-version change.
- [x] #8 A v1 binary rejects a v2 home with its supported `schema-unsupported` behavior, and the v2 binary includes old and current version details where the current typed reason supports them.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Define the storage-only v2 schema and strict verification for restore metadata, recovery state, admitted claim limits/provenance, installations, invites/redemption replay, and reopening attestations.
2. Add one transactional v1-to-v2 migration with stable random restore identity, epoch constraint rebuilding, additive provenance fields, and rollback/concurrent-opener safety.
3. Expose the non-secret restore identity from Store and report it in doctor; extend restored/revoked public lifecycle validation.
4. Add bootstrap, populated migration, failure rollback, concurrent migration, constraint, compatibility, doctor, and event validation tests.
5. Run focused tests and the full repository quality gates, review the diff, record acceptance evidence, and commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented schema v2 and one-way v1 migration. The migration verifies v1 inside the same BEGIN IMMEDIATE transaction, preserves active claims, started/completed operation replay, checkpoints, event/cursor watermarks, and retained epochs, and verifies v2 before commit. Added restore identity, recovery/authentication/invite/reopening/renewal replay storage, admitted-limit and provenance fields, strict state and unique-index verification, doctor reporting, and restored/revoked event validation. Independent review found weak unique-index verification and an invite revocation-state gap; both were fixed with regression coverage. Validation: focused store/doctor/CLI tests passed; mise run ci passed including format, staticcheck, vet, all tests, race tests, man generation, govulncheck, and e2e.

Delivery commit: 32b8be0 (Migrate authority store to schema v2).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Upgraded the shared SQLite store to schema v2 with atomic, rollback-safe v1 migration and preserved local state. Added random stable restore identity, remote provenance and admitted limits, recovery/installations/invites/redemption and renewal replay/reopening storage, strict schema constraints, doctor visibility, and restored/revoked lifecycle validation. Verified with migration/concurrency/failure/compatibility/constraint regressions, independent review, and a passing mise run ci.
<!-- SECTION:FINAL_SUMMARY:END -->
