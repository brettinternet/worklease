---
id: TASK-59.1
title: Record how every ownership epoch ends
status: To Do
assignee: []
created_date: '2026-09-07 15:00'
labels:
  - storage
dependencies: []
references:
  - src/worklease/acquisition.py
  - src/worklease/lifecycle.py
  - src/worklease/garbage_collection.py
  - docs/claim-model.md
parent_task_id: TASK-59
priority: medium
type: enhancement
ordinal: 61000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Persist a terminal record for every ownership epoch so history is complete, not just for voluntary releases.

Today `releases` is written only by `release` and `release-bundle`. Two other endings leave no record of how the prior epoch finished:

- Expiry replacement: `acquire` on an expired resource overwrites the `claims` row in place (`ON CONFLICT(resource) DO UPDATE`) and carries the checkpoint forward as `expired-recovery`, but the replaced epoch keeps only its `epochs` row (acquire-time identity) with no end time, end reason, final revision, last heartbeat, or final checkpoint.
- Transfer: the predecessor is replaced in `claims` and named in the transfer operation receipt, but has no terminal record of its own.

The same applies to bundle members. The terminal record should capture at least: resource, claim ID, end reason (released, transferred, expired-replaced), ended-at authority time, final revision, last heartbeat time, expiry time, retained checkpoint, and successor claim ID when one exists. Reuse or generalize the existing `releases` table rather than adding a parallel structure if the schema allows. Existing `expired-recovery` and `clean-handoff` checkpoint recovery on acquire, gc protections, and `status`, `list`, and `inspect-operation` output must keep their current behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Acquiring a resource whose prior claim expired persists a terminal record for the replaced epoch with reason expired-replaced, its final revision, last heartbeat, expiry time, retained checkpoint, and the successor claim ID
- [ ] #2 Transfer persists a terminal record for the predecessor epoch with reason transferred and the successor claim ID
- [ ] #3 Release and release-bundle terminal records carry the same fields with reason released, and bundle expiry replacement and release record one terminal record per member resource
- [ ] #4 Terminal records never store the claim token and are written in the same transaction as the ownership change so an interrupted operation leaves either both or neither
- [ ] #5 gc dry-run and apply treat all terminal records like releases today: protected inside the retention window and required by current ownership, deleted together otherwise, with existing gc tests still passing
- [ ] #6 Existing checkpoint recovery on acquire, status, list, and inspect-operation output is unchanged, and existing schema migration opens pre-existing databases without data loss
- [ ] #7 Tests cover expiry replacement, transfer, release, bundle variants, interruption atomicity, and migration; docs/claim-model.md describes the terminal record and its reasons
<!-- AC:END -->
