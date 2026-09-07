---
id: TASK-59.3
title: Disclose provenance and coverage in resource history
status: To Do
assignee: []
created_date: '2026-09-07 17:17'
labels:
  - cli
  - storage
  - docs
dependencies:
  - TASK-59.2
references:
  - src/worklease/projections.py
  - src/worklease/sqlite.py
  - src/worklease/schemas/v1/history.json
  - docs/cli-reference.md
  - >-
    https://github.com/jpicklyk/task-orchestrator/blob/main/current/src/main/resources/db/migration/V16__Resource_Lease_History.sql
  - >-
    https://github.com/GoogleCloudPlatform/khi/blob/main/docs/en/guide/03-timeline-view.md
parent_task_id: TASK-59
priority: medium
type: enhancement
ordinal: 64000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the `history` output say which retained record supports each projected fact and how complete the retained window is, so a reader can tell observed facts from synthesized ones and can see when earlier history is missing without the projection inventing events.

This closes the remaining gaps found while comparing TASK-59 against task-orchestrator (append-only hold intervals, documented pre-migration gaps, held-at interval rule) and Kubernetes History Inspector (every revision backed by a specific log, hatched revisions when the start falls outside the retained window). The trust boundary is unchanged: history remains a local, retention-bounded diagnostic projection, not an audit log, attestation, or provider record.

Scope, all additive to the TASK-59.2 projection and schema:

1. Provenance. Every projected event carries a `source` field naming the retained record kind that supports it: `epoch` (synthesized acquisition), `operation`, `reconciliation`, `termination`, or `current-claim`. Events never merge facts from two sources into one field.
2. Completeness. Each epoch carries a `completeness` value of `complete` (acquisition revision and termination both present), `open` (current claim, no termination), or `legacy-incomplete` (acquisition revision unavailable, or neither current nor terminated). Do not add a clock-derived state such as open-expired; expose stored `expiresAt` and let the reader compare.
3. Coverage. Each resource carries a `coverage` block with the earliest retained acquisition revision, the current resource revision from the `resources` tombstone row, and the count of legacy-incomplete epochs. The schema description states that revisions below the earliest retained value were removed by `gc --apply` or predate migration, and that the store cannot distinguish the two.
4. Held-at rule. The schema description documents the interval rule an epoch was held at time T when `acquiredAt <= T < coalesce(effectiveAt, expiresAt)` so consumers compute it consistently. No `--at` flag is added.
5. Index. Add an index on `epochs(resource, acquisition_revision)` so the per-resource projection does not scan the table. Bundle epochs continue to use a `json_each` scan over stored members.

Non-goals: `--at`, `--since`, identity filters, all-resource output, JSON Lines export, append-only triggers, hash chaining, signing, never-prune retention, force-release reasons, and any remote or cross-host semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every event in history --json has a source value from the fixed set epoch, operation, reconciliation, termination, current-claim, and the synthesized acquisition event always reports epoch
- [ ] #2 Each epoch reports completeness as exactly one of complete, open, or legacy-incomplete, derived only from stored fields with no clock input, and repeated runs on an unchanged store remain byte-identical
- [ ] #3 Each resource reports a coverage block with earliestRetainedRevision, resourceRevision, and legacyIncompleteCount; after gc --apply removes older epochs the block reflects the gap without any synthesized event
- [ ] #4 The v1 history schema documents the coverage caveat and the held-at interval rule in field descriptions, and schema tests validate the new fields
- [ ] #5 An index on epochs(resource, acquisition_revision) exists after migration, is created for pre-existing databases without data loss, and the history projection query plan uses it
- [ ] #6 Text output shows source, completeness, and coverage using the documented text grammar; docs/cli-reference.md and docs/claim-model.md describe the three completeness states and the coverage caveat
- [ ] #7 Tests cover every source kind, all three completeness states, coverage before and after gc, legacy null revisions, bundle-member epochs, migration of the index, and schema validation
<!-- AC:END -->
