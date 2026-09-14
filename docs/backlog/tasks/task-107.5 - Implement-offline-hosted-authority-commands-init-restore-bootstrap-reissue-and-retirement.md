---
id: TASK-107.5
title: >-
  Implement offline hosted-authority commands: init, restore, bootstrap reissue,
  and retirement
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:03'
labels:
  - remote-authority
dependencies:
  - TASK-107.3
  - TASK-107.6
references:
  - internal/cli
  - internal/gc
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 137000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement the four offline hosted-authority commands under operating-system or owner-private home authorization and the hosted lock. No HTTP retirement route or admin role grant is part of this task. Initialization creates the hosted database and one bootstrap admin invite. Restore rotates the incarnation, ends active claims as restored while preserving unresolved work, revokes retained credentials and invites, enters recovery mode, and creates a new bootstrap invite. Bootstrap reissue replaces only the bootstrap invite while preserving incarnation, claims, and prior history. Retirement refuses unsafe removal and exports bounded redacted unresolved evidence before a forced retirement.

The private bootstrap secret file and database transaction cannot commit atomically. Use crash-recoverable ordering that durably writes and fsyncs the protected secret before making its database grant usable, and keep `serve` fail-closed until initialization or restore is finalized. TASK-107.6 owns invite and revocation primitives; this task composes them with database initialization and restore in one domain transaction where required.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All four commands require offline OS or owner-private home access, acquire the hosted lock before opening the database, and fail when `serve` or another hosted writer holds it. No HTTP route performs retirement or grants the offline bootstrap authority.
- [ ] #2 Initialization creates a v2 hosted home and exactly one usable admin bootstrap invite, writes the secret only to an owner-private file, prints only redacted identity and path information, and refuses a non-empty home.
- [ ] #3 Initialization and restore use crash-recoverable secret-before-grant ordering with durable file creation and fsync. `serve` refuses an incomplete home, and crash, fsync, database commit, and restart tests leave either no usable grant or one recoverable usable grant without secret disclosure.
- [ ] #4 Restore atomically rotates `restore_id`, ends every active claim as `restored` at restore time, preserves started operations as unresolved, revokes retained installations and invites, sets recovery mode, records the selected durable backup cutoff and initial unknown bounds, and creates a fresh bootstrap invite; restoring the same backup twice yields distinct incarnations.
- [ ] #5 Bootstrap reissue invalidates the prior bootstrap invite while preserving `restore_id`, claims, epochs, operations, receipts, and prior history. It may append the defined audit event but does not rewrite existing events.
- [ ] #6 Retirement refuses while active claims or unresolved started operations remain. Forced retirement first completes a durable redacted export outside the retiring database covering every unresolved record in bounded batches or records, with a count or manifest that detects omission, and includes no credentials, argv, checkpoint bodies, or evidence bodies.
- [ ] #7 Offline composition uses TASK-107.6 authentication and revocation primitives and completes each domain change atomically in the database after the private secret is durable.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
