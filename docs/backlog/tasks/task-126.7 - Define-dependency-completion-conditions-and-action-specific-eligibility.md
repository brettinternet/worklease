---
id: TASK-126.7
title: Define dependency completion conditions and action-specific eligibility
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
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
- [ ] #1 contract.md defines typed relationships (hard prerequisite, parent/child hierarchy, related work, cross-source prerequisite) with source-qualified endpoints and provenance, plus a named completion condition per hard edge. The default condition is the caller-declared terminal state, and callers that supply only `dependencies: WorkRef[]` behave exactly as before
- [ ] #2 Only hard prerequisites affect readiness. Hierarchy and related work never block unless an explicit provider or caller policy declares it, and shared resources are claim contention, never dependency edges
- [ ] #3 Readiness is per candidate: a known unsatisfied prerequisite means blocked, even while other edges are unknown; otherwise incomplete or stale evidence means unknown; ready requires the complete relevant closure, satisfied hard prerequisites, and no provider blocker. Missing, inaccessible, or cyclic prerequisites prevent readiness proof without leaking inaccessible item details
- [ ] #4 A requested completion condition the source cannot verify yields a capability/unknown outcome and never falls back to "closed means satisfied". The raw outcome (for example `not_planned`) is exposed next to its interpretation and is never described as successful implementation
- [ ] #5 Eligibility is action-specific: starting or resuming requires readiness; a verified current owner may still report Blocked or record progress after prerequisites change, without becoming start-eligible and without bypassing ownership, authorization, or receipt verification; completion requires its declared evidence
- [ ] #6 A versioned JSON fixture file (input graph, requested action, and expected readiness or eligibility with reasons) is committed with a short schema note. It covers a hard edge versus hierarchy, a cross-source reference, a cycle, a partial graph with a known blocker, a partial graph without one, an unsupported completion condition, and blocked-owner maintenance. TASK-128.3 consumes it
- [ ] #7 skills/worklease-workflow/SKILL.md, source-workflow.md, the examples, and doc-1 (edited only through `backlog doc update`) stay consistent with the extension
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run doc-test` passes and every changed relative link resolves
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
