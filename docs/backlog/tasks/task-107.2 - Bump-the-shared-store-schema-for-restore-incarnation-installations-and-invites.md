---
id: TASK-107.2
title: >-
  Bump the shared store schema for restore incarnation, installations, and
  invites
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:03'
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
- [ ] #1 `store.SchemaVersion` is 2, and opening a v1 home migrates it once in one `BEGIN IMMEDIATE` transaction. Concurrent openers produce one valid v2 store, and injected failures roll back without a partial schema.
- [ ] #2 Migration tests cover empty and populated v1 homes, including active claims, started operations, replay rows, checkpoints, cursors, and retained history, plus already-v2 opens and concurrent migration.
- [ ] #3 `meta` stores a cryptographically random `restore_id` created at bootstrap or migration and preserved across ordinary reopen; `doctor` reports it beside `authorityId` without exposing a secret.
- [ ] #4 The schema supports `restored` and `revoked` epoch end reasons; per-claim admitted TTL and absolute hold limits; trusted installation attribution separate from agent labels; retained remote and incarnation provenance; recovery mode; bootstrap invite flags, hashes, and replay; and reopening attestations with selected backup cutoff, lost-history interval, unknown bounds, coverage gaps, and private evidence references.
- [ ] #5 `verifySchema` requires the v2 tables and constraints, and the existing public event validation accepts the `restored` and `revoked` end reasons without weakening prior validation.
- [ ] #6 Installation and invite tables store immutable identifiers, roles, non-unique labels, hashes rather than plaintext secrets, issuer and request provenance, revocation and expiry state, and bounded one-time redemption replay with database constraints enforcing valid states.
- [ ] #7 `internal/store` exposes only storage primitives and contains no HTTP types, bearer processing, authorization policy, or server configuration policy. Existing local behavior and tests remain valid apart from the schema-version change.
- [ ] #8 A v1 binary rejects a v2 home with its supported `schema-unsupported` behavior, and the v2 binary includes old and current version details where the current typed reason supports them.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
