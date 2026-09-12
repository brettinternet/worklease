---
id: TASK-69
title: Add a global paginated history event feed
status: To Do
assignee: []
created_date: '2026-09-12 02:01'
labels: []
dependencies: []
priority: medium
type: feature
ordinal: 73000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Operators cannot currently discover retained ledger activity without already knowing an exact resource: `worklease history` requires `--resource` and returns the epoch projection for one resource. Add a bounded global history mode so operators and agents can inspect recent lifecycle activity across resources, while preserving the existing resource-scoped history contract. This is a feed of Worklease-generated lifecycle records, not a facility for posting human-authored messages.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease history` without `--resource` succeeds and returns retained acquisition, operation, reconciliation, and termination records across singleton and bundle claims; every record identifies its resource or ordered resources, source/kind, relevant claim and operation identity, and event timestamp.
- [ ] #2 The global feed is ordered newest-first with documented deterministic tie-breakers, defaults to 100 records, accepts `--limit` from 1 through 1000, and rejects invalid limits through the normal stable CLI error envelope without reading or mutating ledger state.
- [ ] #3 Global history supports an opaque `--cursor`; responses expose whether more records exist and a next cursor in JSON, and an actionable continuation hint in text, only when another page exists.
- [ ] #4 Pagination is snapshot-stable: traversing a cursor chain has no duplicates or omissions from its captured result set when newer ledger records are written concurrently, and cursors are bound to the query and options so incompatible, malformed, or stale cursor reuse fails with a stable documented error.
- [ ] #5 Existing `worklease history --resource R` text and JSON behavior remains backward compatible; unsupported combinations of resource-scoped history with global pagination options fail clearly rather than silently changing the resource projection.
- [ ] #6 Packaged JSON schemas, CLI help and reference documentation, README examples, and changelog describe global history, limits, cursor continuation, ordering, retention and GC gaps, and that records are lifecycle events rather than postable messages.
- [ ] #7 Tests cover empty and multi-resource ledgers, singleton and bundle records, every included event source, deterministic ordering ties, default, minimum, and maximum limits, multi-page traversal, concurrent inserts between pages, exhausted cursors, invalid and cross-query cursors, redaction, read-only behavior, and compatibility of resource-scoped history.
<!-- AC:END -->
