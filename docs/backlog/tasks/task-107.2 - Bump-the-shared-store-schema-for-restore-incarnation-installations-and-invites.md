---
id: TASK-107.2
title: >-
  Bump the shared store schema for restore incarnation, installations, and
  invites
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies: []
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
Every remote invariant needs rows the v1 schema cannot hold: a `restore_id` meta value, a recovery-mode state, `restored` and `revoked` epoch end reasons (`epochs.end_reason` is CHECK-constrained to released, transferred, and expired in `internal/store/schema.go`), and `installations`, `invites`, and reopening-record tables for authentication and restore. The local CLI shares this store, so this is a one-way migration of every existing home, not a hosted-only table set; the design explicitly says the local release cannot be exempted.

Keep `internal/store` storage-only: no HTTP, authentication, or serve types may appear in the migration or schema code. The `restore_id` value is a cryptographically random identifier compared for equality only, never ordered, because restoring one backup twice must yield two distinct incarnations.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `store.SchemaVersion` is 2; opening a v1 home with the new binary migrates it once inside one `BEGIN IMMEDIATE` transaction, and a v1 binary refuses a v2 home with `schema-unsupported` naming both versions.
- [ ] #2 `meta` gains a cryptographically random `restore_id` at bootstrap and during migration; close and reopen preserve it; `doctor` reports it under `authority.identity` beside the authorityId without exposing any secret.
- [ ] #3 `epochs.end_reason` accepts `restored` and `revoked`, `verifySchema` requires the new constraint and tables, and both reasons produce valid public events through the existing event validation.
- [ ] #4 `installations` (immutable id, role, non-unique label, credential hash, created and revoked timestamps, issuing invite), `invites` (code hash, role, label, issuer, request id, expiry, redemption replay result), a recovery-mode row, and a reopening-record table exist with constraints that forbid plaintext secrets and allow at most one redemption per invite.
- [ ] #5 Existing local tests pass unchanged apart from the version constant, and migration tests cover an empty home, a populated v1 home, an already-v2 home, and two openers racing the migration.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
