---
id: TASK-59
title: Add a local claim history ledger
status: To Do
assignee: []
created_date: '2026-09-07 14:59'
labels:
  - storage
  - cli
  - docs
dependencies: []
references:
  - docs/distributed-cloudflare-claim-authority.md
  - docs/claim-model.md
  - src/worklease/sqlite.py
  - src/worklease/garbage_collection.py
priority: medium
type: feature
ordinal: 60000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the local SQLite store a readable ledger of who held each resource, when, and what they attested, so a human or another agent can answer "which agent worked on this resource, in what order, and how did each ownership epoch end" without a remote service.

The store already persists the pieces: `epochs` (identity at acquire), `operations` (every acquire, heartbeat, checkpoint, transfer, exec, and release receipt with a timestamp), `releases` (voluntary release with retained checkpoint), and `reconciliations`. Three gaps stop this from being a ledger:

1. When an expired claim is replaced by a new acquire, the `claims` row is overwritten in place and nothing records how the prior epoch ended, its final heartbeat, or its final checkpoint. Only voluntary release writes a terminal record.
2. No command joins these tables into an ordered timeline. `list` shows current and expired claims only; `inspect-operation` needs an operation ID; `gc` inventories what would be deleted.
3. `gc` deletes operations, releases, and epochs after 30 days by default, so history must be exportable before retention removes it.

This is the local precursor to the evidence-ledger framing in the distributed authority design. It uses only existing local records and adds no remote authority, no hash chaining, and no provider writes. Subtasks deliver the terminal record first, then the read and export surface.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every ownership epoch, singleton or bundle member, has a durable terminal record whether it ended by release, transfer, or expiry replacement
- [ ] #2 A single CLI command shows an ordered, token-redacted timeline of epochs and lifecycle events for a resource, with machine-readable JSON validated by a versioned schema
- [ ] #3 Documentation explains the history model, its retention boundary under gc, and how to archive history before collection
<!-- AC:END -->
