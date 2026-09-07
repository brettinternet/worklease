---
id: TASK-51
title: Distinguish an invalid bearer token from a lost claim
status: To Do
assignee: []
created_date: '2026-09-07 03:28'
labels:
  - cli
  - api
dependencies: []
references:
  - src/worklease/store.py
priority: medium
type: enhancement
ordinal: 52000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ownership checks raise `stale-claim` when either the claim ID or the token mismatches. A truncated token file or mis-plumbed `--token-fd` therefore reports the same reason as genuine ownership loss, and the skill tells agents to stop on ownership loss. Propose a distinct reason (for example `invalid-token`) when the claim ID matches but the token does not, keep exit code 2, and add it to schemas, the README reason list, and the skill guidance. Adding a reason is a minor-version API change; document it in the compatibility notes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 heartbeat with the correct claim ID and a wrong token returns a distinct documented reason with exit 2 and no token material in output
- [ ] #2 heartbeat with a wrong claim ID still returns stale-claim
- [ ] #3 Schemas, README exit-code and reason documentation, and skills/worklease-workflow safe-failure guidance mention the new reason; tests cover both paths
<!-- AC:END -->
