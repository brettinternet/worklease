---
id: TASK-142.5
title: Sync and reconcile Linear source and relations in queue index
status: Done
assignee: []
created_date: '2026-09-25 16:30'
updated_date: '2026-09-25 19:40'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.4
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: feature
ordinal: 69000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Integrate Linear source refresh with the existing per-quota scheduler and disposable index. Relation invalidation and pagination semantics come from the recorded probe, not assumptions about updatedAt.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Incremental pages and relation reconciliation preserve complete graph evidence and respect per-organization/account request and complexity limits
- [x] #2 Interrupted or partial scans never advance a watermark or prove deletion or permission loss; tests cover changed page ordering and relation removal
- [x] #3 D21 scale fixture benchmark records request counts and latency, and §14 assumptions are updated if measured results differ
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add Linear-specific, partitioned resume metadata and bounded overlapping issue scans through the existing per-account quota queue; never promote partial visibility to complete or infer absence from a moving cursor.
2. Reconcile each issue’s relation directions independently of updatedAt; preserve old complete closure until a fresh complete read, and keep interrupted graph evidence unknown.
3. Cover reordered pages, interrupted scans, relation removal and quota exhaustion with tests; run a 10k D21 fixture benchmark and record request/latency implications in §14.
4. Run required checks, review scoped risks once, commit, integrate into main, and finalize the task from the primary checkout.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented metadata-only principal-scoped Linear sync cursors and overlapping updatedAt windows, 5-page refresh bound, independent 50-issue relation sweep with persisted offset, partial coverage/absence safeguards and fail-closed access handling. 10k localhost benchmark: 40 list + 100 relation requests for a 50-issue sweep; 197/263/470 ms across three samples. Focused race checks and format/lint/typecheck/full test passed on worktree with isolated Go build cache; an initial unrelated CLI timing failure passed on rerun, then full suite passed. Review in progress.

One general review found two concrete defects: a superseded worker could checkpoint an unpublished page, and summary replacement could discard prior complete edges when relation reread failed. Moved Linear checkpoint after successful generation-gated publication, retained prior edges as explicitly incomplete hints, and added interleaved-refresh/partial-reread tests. Also cleared the old window boundary on cold restart and confined ambiguous item read loss to that item, with focused tests. Focused race and format/lint/typecheck passed after corrections; rerunning full suite and staged hooks before merge.

Delivered code b67a03b; merged to main as ea94bd8. On main, race -count=3 passed for Linear sync/reordering/relation-removal, superseded-refresh and schema migration; 10k benchmark repeated at 95/100/114 ms with 40 list and 100 relation requests per run. Main format-check, lint, typecheck, and full test passed. Worktree hooks passed before commit. The committed timestamp is a completed-window boundary only, not source-completeness proof: interrupted windows leave it unchanged, and even finished moving-cursor traversals keep source coverage partial. No absence is classified as deletion or permission loss.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added principal-scoped bounded Linear sync, overlapping windows and independent relation sweeps with fail-closed partial evidence. Verified interrupted/reordered scans, relation removal, quota handling and migration by focused race tests; measured 10k fixture; all main gates and staged hooks passed. Code b67a03b, merge ea94bd8.
<!-- SECTION:FINAL_SUMMARY:END -->
