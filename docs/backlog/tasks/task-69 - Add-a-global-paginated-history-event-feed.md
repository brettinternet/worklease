---
id: TASK-69
title: Add a paginated cross-resource events command
status: To Do
assignee: []
created_date: '2026-09-12 02:01'
updated_date: '2026-09-12 02:16'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/projections.py
  - src/worklease/cli.py
  - src/worklease/cli_dispatch.py
  - src/worklease/sqlite.py
  - src/worklease/schemas/v1/history.json
  - src/worklease/schemas/v1/commands.json
  - src/worklease/schemas/v1/index.json
  - scripts/release_docs.py
  - docs/cli-reference.md
  - docs/claim-model.md
  - tests/test_history.py
  - tests/test_schemas.py
priority: medium
type: feature
ordinal: 73000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Operators and agents cannot see recent ledger activity without already knowing an exact resource: `worklease history` requires `--resource` and returns the nested epoch projection for that one resource, and `worklease list` shows only current claims. A bounded, newest-first feed of retained lifecycle records across all resources answers "what happened here recently" without a resource key. Records are Worklease-generated lifecycle rows, never human-authored entries.

## Decisions (settled here; implement, do not re-open)

- **New command, not a `history` mode.** `history` is a documented per-resource epoch projection with its own JSON schema (`history.json` requires `resource`, `coverage`, `epochs` on success) and text grammar; a flat feed under the same operation name would fork both. Add `worklease events` in the "Inspection and recovery" group. `history` is unchanged and keeps requiring `--resource`; the reference sentence saying it provides no all-resource output or pagination points to `events` instead.
- **Sources.** One record per retained row from `epochs` and `bundle_epochs` (source `epoch`, kind `singleton` or `bundle`, timestamp `acquired_at`), `operations` (source `operation`, timestamp `created_at`), `reconciliations` (source `reconciliation`, timestamp `reconciled_at`), and `epoch_terminations` (source `termination`, timestamp `recorded_at`). The `releases` table is not a separate source because every release path also records a termination with reason `released`. Current claim snapshots are state, not events; `list` covers them. Per-source field allowlists are exactly what `history` already emits for `Epoch`, `Operation`, `Reconciliation`, and `Termination`; every record additionally carries `at` (the sort timestamp) and `resource` (singleton or member resource) or ordered `resources` (bundle epochs).
- **Ordering.** Newest-first by `at`, then `source` in the fixed order `termination`, `reconciliation`, `operation`, `epoch`, then a normalized `resourceKey` (the singleton resource or compact JSON encoding of ordered bundle resources), `claimId`, `operationId`, and `kind` ascending. The normalized key includes every column needed to distinguish retained rows, so the order is total and stable.
- **Pagination.** Keyset, not offset. `--limit N` (1 through 1000, default 100) is the page size; the projection fetches N+1 rows to learn whether more exist. `--cursor` is an opaque URL-safe base64 encoding of a compact JSON object holding a cursor format version and the last returned sort key; later pages select rows strictly older in the total order. The cursor is not bound to `--limit`, so page size may change mid-traversal. JSON carries `hasMore` (boolean) and `nextCursor` (string or null); text prints `NEXT_CURSOR` and a copyable `HINT` continuation line only when another page exists. Malformed, truncated, or wrong-version cursors fail `invalid-cursor` (exit 64) before the store is opened.
- **Consistency guarantee, stated honestly.** Keyset pagination guarantees no duplicates and no skips among rows that exist for the whole traversal. A row recorded during traversal appears only if its sort key is older than the current position; under normal wall-clock progression concurrent writes land at the head and a continued chain never shows them. Rows removed by `gc --apply` between pages disappear. There is no cross-invocation snapshot and the documentation must not imply one. `expired` terminations recorded by GC sort by `recorded_at`, which can be long after `effective_at`; both fields are emitted.
- **Read-only.** Use `connect_readonly`, the same symlink and regular-file checks as `history`, and a deferred transaction; never read `token`, `request`, `receipt`, `evidence`, or checkpoint bodies. Add `epochs_by_acquired_at` and `bundle_epochs_by_acquired_at` indexes with `CREATE INDEX IF NOT EXISTS` (additive, no schema version bump); the other sources already have timestamp indexes from GC.
- **Options.** `events` takes `--limit`, `--cursor`, `--full` (same meaning as on `list`: complete resource strings and identifiers instead of the compact rendering; reuse the `list` helpers established by TASK-68), plus the standard output and `--home` options. No `--resource`, time filters, or source filters in this task.
- **Surface.** New `events.json` schema, `events` in the `commands.json` and `index.json` enums, `_COMMANDS`, the text renderer table, `_COMMAND_GROUPS` in `scripts/release_docs.py`, the operation table in `docs/claim-model.md`, and the text grammar table in `docs/cli-reference.md`. `LeaseStore.events(...)` becomes public API alongside `history`.

## Non-goals

Filtering, time windows, any change to `history`, live tailing, exposing current claims, MCP tools, and any notion of posting messages.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease events` succeeds on an empty ledger (zero records, `hasMore` false, `nextCursor` null) and on a populated ledger returns one record per retained epoch, bundle epoch, operation, reconciliation, and termination row; each record carries `source`, `at`, `resource` or ordered `resources`, and the allowlisted identity fields for its source, and never a token, request, receipt, evidence, or checkpoint body.
- [ ] #2 Records are newest-first by the total order in the description; records with identical timestamps follow the documented tie-breakers, and JSON output is byte-identical across repeated reads of unchanged state.
- [ ] #3 `--limit` defaults to 100, accepts 1 through 1000, and rejects 0, negatives, non-integers, and values above 1000 with exit 64 through the standard error envelope without opening the state database.
- [ ] #4 When more records exist, JSON has `hasMore: true` and a string `nextCursor`, and text prints `NEXT_CURSOR` plus a `HINT` with the copyable continuation command; when none remain, `hasMore` is false, `nextCursor` is null, and text prints neither.
- [ ] #5 Following the cursor chain to exhaustion returns every row that remains present throughout the traversal exactly once, including when `--limit` changes between pages. Rows inserted after page one follow their sort key: newer head rows are excluded from the continued chain, while an explicitly backdated row older than the cursor may appear; rows collected between pages may disappear.
- [ ] #6 Malformed, truncated, wrong-version, or structurally invalid cursors fail `invalid-cursor` (exit 64) before the store is opened; a valid cursor with modified but well-typed sort-key values is treated as a caller-supplied position, and a cursor past every remaining record returns an empty page with `hasMore` false.
- [ ] #7 `events` is read-only: it uses the read-only connection, refuses symlinked state files with `state-file-is-symlink`, returns the empty result when no database exists, creates no files, and its queries never touch secret-bearing columns (verified the same way as the existing history secret-column test).
- [ ] #8 `history --resource R` text and JSON output are byte-for-byte unchanged, `history` still requires `--resource`, and `events` rejects `--resource` as an unrecognized argument.
- [ ] #9 `events.json` validates success and error payloads, `commands.json` and `index.json` list `events`, `scripts/release_docs.py` renders with `events` in the inspection group, `docs/cli-reference.md` and `docs/claim-model.md` document the command, ordering, cursor semantics, consistency caveats, and retention gaps, README has one example, and CHANGELOG `Unreleased` has an Added entry.
- [ ] #10 Tests cover empty and multi-resource ledgers, singleton and bundle records, every source, timestamp ties, default, minimum, maximum, and invalid limits, multi-page traversal, newer and explicitly backdated writes between pages, a changed page size mid-traversal, exhausted and invalid cursors, redaction, read-only behavior, the new indexes, and `history` compatibility; `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
