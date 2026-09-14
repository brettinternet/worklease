---
id: TASK-107.5
title: >-
  Implement offline hosted-authority commands: init, restore, bootstrap reissue,
  and retirement
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies:
  - TASK-107.3
  - TASK-107.4
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
Some hosted operations must run with the server stopped and the single-writer lock held, because they change incarnation or administrative state that no live request may observe mid-way. The design fixes four. First-start init creates the database and writes exactly one `admin` bootstrap invite to an owner-private file, printing only the authorityId and file path so the secret never reaches daemon logs. Restore treats the database as authority recreation, never resumption: rotate `restore_id`, end every active claim `restored` effective at restore time leaving started operations unresolved, revoke every retained installation and invite, set recovery mode, and write a fresh bootstrap invite. Bootstrap reissue replaces a lost or expired bootstrap invite without touching incarnation, claims, or history. Retirement refuses while active claims or unresolved started operations exist and, when forced, first exports the redacted unresolved list for the deployment administrative log.

Restoring the same backup twice must produce two distinct incarnations. None of these commands may run while a `serve` holds the home.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease serve init` on an empty home creates a v2 database, writes exactly one `admin` invite to a 0600 file, prints the authorityId and the file path and never the code, and refuses a non-empty home.
- [ ] #2 `worklease serve restore` on a copied database, in one transaction, rotates `restore_id`, ends every active claim with `restored` effective at restore time leaving started operations unresolved, revokes every retained installation and invite, sets recovery mode, and writes a fresh bootstrap invite; running it twice on the same backup yields two different `restore_id` values and requests bound to the first are rejected by the second.
- [ ] #3 Bootstrap reissue invalidates the prior bootstrap invite and changes nothing else; `restore_id`, claims, epochs, and events are byte-identical before and after.
- [ ] #4 Retirement refuses while active claims or unresolved started operations exist; `--force` writes a redacted export of unresolved operations containing no credentials, argv, checkpoint bodies, or evidence before retiring the database.
- [ ] #5 Each command fails closed when the hosted lock cannot be taken, and tests prove none runs against a home whose `serve` is live.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
