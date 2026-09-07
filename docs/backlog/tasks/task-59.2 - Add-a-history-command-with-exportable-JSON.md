---
id: TASK-59.2
title: Add a history command with exportable JSON
status: To Do
assignee: []
created_date: '2026-09-07 15:00'
labels:
  - cli
  - docs
dependencies:
  - TASK-59.1
references:
  - src/worklease/cli.py
  - src/worklease/projections.py
  - src/worklease/schemas/v1/list.json
  - src/worklease/schemas/v1/commands.json
  - docs/cli-reference.md
parent_task_id: TASK-59
priority: medium
type: feature
ordinal: 62000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add a read-only `history` command that joins epochs, terminal records, operations, and reconciliations into one ordered timeline so a human or agent can see who held a resource, in what order, what they checkpointed, and how each epoch ended.

No existing command answers this. `list` shows current and expired claims only, `inspect-operation` needs a known operation ID, and `gc` reports what would be deleted. The underlying tables already hold the events with authority timestamps.

Scope:

- Filter by exact resource, and optionally by agent, session, owner, work key, and a since/until time window. With no filter, list all resources in the store.
- Present epochs in acquisition order, each with its lifecycle events (acquire, checkpoint, transfer, exec, reconciliation, terminal end) in authority-time order. Collapse heartbeats to a count and last time by default; `--full` shows each one.
- Follow the existing output conventions: human-readable text by default using the documented text grammar, `--json` producing a schema-versioned envelope, and a new `history.json` v1 schema registered in `commands.json` and `index.json` and covered by the schema tests.
- Redact tokens and any secret-bearing receipt content exactly as `status` and `list` do. Command output and raw provider payloads inside exec receipts are not shown.
- JSON output is deterministic for a given store state so it can be archived before `gc` runs. Document that `gc` is the retention boundary for history and show the archive-then-collect sequence in the retention section of docs/cli-reference.md.

Non-goals: no hash chain, no signing, no remote authority, no new write path, and no incremental cursor beyond the since/until filters.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 history --resource R prints every ownership epoch for R in acquisition order with its events in authority-time order, ending with the terminal record and reason, and covers singleton and bundle-member epochs
- [ ] #2 Filters for agent, session, owner, work key, since, and until narrow the output; omitting --resource lists all resources; unknown or malformed filters fail with stable schema-versioned errors
- [ ] #3 Heartbeats are collapsed to a count and last time by default and expanded individually with --full
- [ ] #4 Output never contains claim tokens, raw exec output, or provider payloads, verified by a test that seeds each kind of secret-bearing record
- [ ] #5 history --json validates against a new v1 history schema registered in commands.json and index.json, and repeated runs on an unchanged store produce byte-identical JSON
- [ ] #6 docs/cli-reference.md documents the command, its text grammar, and an archive-history-then-gc sequence in the retention section; README mentions the command where list is introduced
- [ ] #7 Tests cover ordering across expiry replacement, transfer, and release, each filter, heartbeat collapsing, redaction, and schema validation
<!-- AC:END -->
