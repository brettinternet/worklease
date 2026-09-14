---
id: TASK-107.12
title: Document and promote the experimental remote authority
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies:
  - TASK-107.11
references:
  - README.md
  - CHANGELOG.md
  - docs/cli-reference.md
  - docs/mcp.md
  - cmd/worklease-doc-test
  - skills/worklease-workflow/SKILL.md
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: medium
type: docs
ordinal: 144000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Once the harness passes, the capability is promoted as experimental. Users need a remote setup guide (profiles, enrollment, MCP), operators need a `serve` guide with the deployment rules the design imposes (one process per namespace, stop-before-start upgrades, single-host filesystem, optional replication, restore and reopening procedure, retirement, no high-availability claim), and the normative documents must stop describing the invariants as non-existent. `docs/remote-claim-authority.md` becomes the design record with a shipped-experimental status; contract section 20 is amended through section 15; README, CHANGELOG, skills, and the release job follow.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `docs/` gains a remote setup guide and a `serve` operator guide covering deployment rules, a restore and reopen runbook outline, and the explicit non-goals; `docs/cli-reference.md` and `docs/mcp.md` document every new command, flag, reason, and exit family; `cmd/worklease-doc-test` validates the new guides.
- [ ] #2 `docs/remote-claim-authority.md` states a shipped-experimental status with the follow-up list intact, and contract section 20 no longer says the invariants do not exist, recorded through a section 17 entry and a TASK-85 comment.
- [ ] #3 README and CHANGELOG describe the experimental remote authority, its opt-in nature, and the measured binary-size delta on every release target; the release job builds and smoke-tests `serve` on each target.
- [ ] #4 `skills/worklease-workflow` reflects remote profile selection without changing the provider-neutral coordination contract.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
