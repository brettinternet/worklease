---
id: TASK-126.7
title: Define dependency completion conditions and action-specific eligibility
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 16:10'
labels:
  - work-queue
milestone: m-1
dependencies: []
references:
  - skills/worklease-workflow/references/contract.md
  - skills/worklease-workflow/references/source-workflow.md
  - skills/worklease-workflow/SKILL.md
  - skills/worklease-workflow/examples
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-126
priority: high
type: docs
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The generic contract models dependencies as `dependencies: WorkRef[]` plus item-level `isTerminal`. That cannot express edge-specific completion (closed versus merged versus approved), separate hard prerequisites from hierarchy or related work, or let a blocked owner document the blocker (plan section 7, "Dependency interpretation", section 8, "Focused operational editing", D23, D27). The queue core (TASK-128.3) and next-ready (TASK-130.4) implement whatever this task defines, so ambiguity here becomes inconsistent readiness later.

Extend the contract compatibly. Existing callers that supply only `dependencies` and terminal state must keep their current behavior. Start with named, evidenced conditions, never arbitrary predicates or workflow scripts. Dependency editing, edge inference, OR/conditional workflows, and scheduling stay out of scope.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 contract.md defines typed relationships (hard prerequisite, parent/child hierarchy, related work, cross-source prerequisite) with source-qualified endpoints and provenance, plus a named completion condition per hard edge. The default condition is the caller-declared terminal state, and callers that supply only `dependencies: WorkRef[]` behave exactly as before
- [x] #2 Only hard prerequisites affect readiness. Hierarchy and related work never block unless an explicit provider or caller policy declares it, and shared resources are claim contention, never dependency edges
- [x] #3 Readiness is per candidate: a known unsatisfied prerequisite means blocked, even while other edges are unknown; otherwise incomplete or stale evidence means unknown; ready requires the complete relevant closure, satisfied hard prerequisites, and no provider blocker. Missing, inaccessible, or cyclic prerequisites prevent readiness proof without leaking inaccessible item details
- [x] #4 A requested completion condition the source cannot verify yields a capability/unknown outcome and never falls back to "closed means satisfied". The raw outcome (for example `not_planned`) is exposed next to its interpretation and is never described as successful implementation
- [x] #5 Eligibility is action-specific: starting or resuming requires readiness; a verified current owner may still report Blocked or record progress after prerequisites change, without becoming start-eligible and without bypassing ownership, authorization, or receipt verification; completion requires its declared evidence
- [x] #6 A versioned JSON fixture file (input graph, requested action, and expected readiness or eligibility with reasons) is committed with a short schema note. It covers a hard edge versus hierarchy, a cross-source reference, a cycle, a partial graph with a known blocker, a partial graph without one, an unsupported completion condition, and blocked-owner maintenance. TASK-128.3 consumes it
- [x] #7 skills/worklease-workflow/SKILL.md, source-workflow.md, the examples, and doc-1 (edited only through `backlog doc update`) stay consistent with the extension
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run doc-test` passes and every changed relative link resolves
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Define compatible typed relationship and named-condition semantics in the generic contract with per-candidate readiness and action eligibility. 2. Add versioned fixture vectors and update source-workflow, examples, skill, generated Backlog guide through CLI, and proposal decisions. 3. Validate doc-test, links, project quality gates and hooks; finalize with evidence, commit and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented typed relationship/condition contract, action-specific eligibility, v1 fixture vectors and schema note, example/skill/source guide updates, proposal D23/D27 and §7/§8. Updated doc-1 via backlog CLI in primary checkout and copied generated result into isolated branch. Verified JSON, links, git diff --check, lint, format-check, test, typecheck, doc-test and staged hooks. Integration pending unrelated main checkout edits from parallel tasks.

Branch task-126-7-dependency-conditions commits a6f1dc6 (contract, fixtures, proposal, examples) and 0f23c42 (Backlog CLI-generated doc-1 change only). Branch clean. Primary checkout remains dirty with unrelated overlapping work (doc-1, contract, skill, proposal), so merge is deferred until those changes are committed or otherwise resolved; do not overwrite them. Next: refresh primary state, merge when safe, rerun checks, finalize and release.

Acceptance verification: contract per-edge fields/legacy projection and selection rules reviewed against all nine v1 input/output vectors; JSON parsed; docs link walk resolved all relative links (URL-decoded); mise lint/format-check/test/typecheck/doc-test passed; staged hooks passed; doc-1 CLI read-back verified. DoD decision update D23/D27 and proposal §§7–8 is in a6f1dc6.

AC7 remains open until the branch is integrated with the separately completed doc-1 remote-authority/cancellation changes; branch commit intentionally excludes those unrelated edits and the primary checkout carries the CLI-updated composite doc.

2026-09-23 11:27 UTC: Resumed integration check. Prior claim history shows released ownership and branch 0f23c42 clean, but primary main still has uncommitted overlapping edits to doc-1, contract, skill, and proposal, plus unrelated task and script changes. Merge cannot safely update main without disturbing those changes. No branch changes made. Next: once primary checkout overlapping files are committed/cleared by their owners, integrate branch, rerun checks, finalize AC7 and release.

Integrated a6f1dc6 and 0f23c42 into main at c16c0e0; CLI view of doc-1 and cross-check of skill, source-workflow and examples confirm consistent hard-edge and action-eligibility rules. Full lint, format-check, test, typecheck, doc-test and staged hooks passed on integrated main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Integrated typed dependency eligibility contract and v1 vectors; verified guide, examples, doc-1 and full project gates on main.
<!-- SECTION:FINAL_SUMMARY:END -->
