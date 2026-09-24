---
id: TASK-130.4
title: Add `worklease queue next`
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 12:52'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-129
references:
  - skills/worklease-workflow/references/contract.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-130
priority: high
type: feature
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Agents need the contract's `selectNext` implemented once, instead of every agent reimplementing dependency- and claim-aware selection. `worklease queue next --view NAME --json` returns one candidate or a structured no-work reason (plan section 13). It never acquires: the caller claims with the returned resources, and on contention it queries again.

D23: next-ready requires full enumeration and the complete edge set for the scope. While edges are incomplete, readiness is unknown, and `next` must not pick from a partial first page. Plan section 17 asks whether `next` needs provider-neutral claim paging; answer that before relying on List.

D27 also asks the queue to explain parallel-ready groups. The contract's `selectWave` provides this: a group of ready items with no exact-resource overlap. A group is a current observation, not reserved capacity or permission to launch a worker wave.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease queue next --view NAME --json` implements contract.md `selectNext` over the view's complete scope and returns one candidate with its ref, resources, keyInputs, readiness evidence, and claim observation
- [ ] #2 When the scope or edges are incomplete, it returns a structured `incomplete` result that states the coverage. It never returns a candidate chosen from a partial page
- [ ] #3 No-work results distinguish complete-and-empty, all blocked, all claimed, all assigned elsewhere (the default excludes items assigned to others), and incomplete
- [ ] #4 Order follows contract.md exactly: explicit selector and source order within the ready wave, then only the documented priority, order, and stable WorkRef tie-breakers. Eligible unclaimed in-progress items are resumable when the caller marks them eligible, and no other class ordering is invented
- [ ] #5 A group option implements `selectWave` and returns a bounded set of ready items with no exact-resource overlap, explaining each exclusion (prerequisite or resource conflict). It reserves nothing and launches nothing, as tested with two ready items sharing a resource versus two independent ones
- [ ] #6 The plan section 17 claim-paging question is answered in the plan before this task completes
- [ ] #7 Tests cover each no-work reason, incomplete edges, tie-breaking, and cross-source ordering
- [ ] #8 Plain `queue next` never acquires; its output says so and points to `--claim` (TASK-130.5, D28) for agent loops. docs/queue.md shows both the `--claim` loop and the manual query, claim, re-query loop
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reuse the queue snapshot and completeness machinery for complete-scope next and wave selection; distinguish every no-work reason and preserve configured ordering.
2. Add JSON/plain CLI output and tests for coverage, eligibility, ordering, resource conflicts, and non-acquisition.
3. Resolve claim-paging question, document manual and --claim loops, run project gates, review, commit, merge, and finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented complete-snapshot next/wave selection, explicit item ordering, no-work classification, CLI output, focused tests and documentation. Section 17 claim paging answered: bounded status batches cover scoped resources; no authority-wide enumeration needed. Focused queue/CLI tests pass; running full gates and review next.

Commit 5be7027 contains implementation. Reviewer found exact generic resource redaction, explicit-assignment override, and terminal-only classification defects; all fixed with regression tests. Final full gates and staged hooks passed. Integrating into main next.
<!-- SECTION:NOTES:END -->
