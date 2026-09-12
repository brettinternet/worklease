---
id: TASK-85.11
title: Implement read views and garbage collection
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.9
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/garbage_collection.py
  - src/worklease/projections.py
  - tests/test_gc.py
  - tests/test_cli.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 103000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Operators and agents need concise, truthful views of the authority and a bounded way to prune it. The Python proof of concept accumulated verbose field dumps and then compact modes (TASK-68 through TASK-74); the contract asks for concise deterministic text by default, `--full` for detail, and stable JSON. Garbage collection must never remove active or unresolved records and must report gaps honestly (contract section 11).

Read first: contract sections 4 (status, list, history, events, gc rows), 6, 7.2, 11, 18. Python evidence: `src/worklease/garbage_collection.py` (cutoff rules, protection predicates, atomic apply, category summaries), `projections.py` text renderers (what to avoid); `tests/test_gc.py` (all 29 tests, especially test_apply_is_atomic_and_preserves_resource_revision, test_cutoff_is_strict_and_excludes_boundary_records, test_expired_claim_is_retired_only_after_strict_cutoff, test_old_expired_claim_with_unresolved_operation_is_explained, test_active_claim_protects_old_records, test_gc_serializes_expired_retirement_acquire_and_heartbeat, test_dry_run_does_not_mutate_records_or_revisions, test_history_coverage_tracks_partial_and_complete_epoch_gc); `tests/test_cli.py`: test_text_status_uses_compact_operational_summary, test_text_list_aligns_columns_across_multiple_rows, test_display_width_counts_terminal_columns, test_resource_shortening_bounds_opaque_values, test_gc_text_dry_run_is_compact_and_actionable, test_gc_cli_fixed_clock_covers_dry_run_apply_noop_and_json.

Deliver in `internal/gc`: `Plan(cutoff)` computing eligible and protected categories (endedEpochs, expiredClaims, protectedActive, protectedUnresolved) with counts and oldest and newest timestamps; `Apply` running the whole plan in one BEGIN IMMEDIATE transaction, retiring expired claims with `expired-retired` events, pruning events below the minimum retained acquired_seq, raising meta.pruned_through_seq, and appending `gc-applied`; cutoff parsing (RFC 3339, at or before now) with the retention-days default from config. Deliver in `internal/cli`: `gc` (dry run by default, `--apply`, `--retention-days`, `--cutoff`) with an actionable apply hint in text; polish `status`, `list`, `history`, and `events` text: one summary line, compact ordered resources with bounded shortening that preserves path anchors, aligned columns using terminal display width for Unicode, `--full` restoring complete values, deterministic ordering; JSON unchanged from TASK-85.7 and TASK-85.9 except documented additions.

Owned paths: `internal/gc`, `internal/cli/gc.go`, and the text renderers in `status.go`, `list.go`, `history.go`, `events.go` with tests. Out of scope: changing storage semantics, watch, doctor.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Fixed-clock gc tests prove a dry run reports eligible endedEpochs and expiredClaims with counts and oldest and newest timestamps, protects active claims and every epoch or claim with a started operation (reported under protected with the reason), mutates nothing (row counts, revisions, and pruned_through_seq unchanged), and applies a strict cutoff so a record exactly at the cutoff is retained.
- [ ] #2 Apply tests prove everything eligible is removed and every expired claim past the cutoff is retired with an expired-retired event and an epoch end effective at its stored expiry in one transaction, an injected failure mid-apply rolls back completely, pruned_through_seq rises to the highest pruned seq so a later `events --cursor` below it reports the gap, and a concurrent subprocess acquire during apply observes either the pre-apply or post-apply state, never a partial one.
- [ ] #3 `gc --apply` on a retired three-resource claim removes all of its rows atomically, its resources become acquirable afterwards, and history coverage for a pruned resource reports retainedFromSeq correctly.
- [ ] #4 View tests with a fixed clock golden-test text for empty and populated status, list, history, and events, covering a one-resource and a three-resource claim, a 200-character resource shortened with its path anchor preserved, Unicode resource names at 60 and 120 columns, --full restoring complete values, and identical JSON between default and --full for fields present in both.
- [ ] #5 Redaction assertions confirm no view or gc output contains tokens, hashes, checkpoints, or evidence, and `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
