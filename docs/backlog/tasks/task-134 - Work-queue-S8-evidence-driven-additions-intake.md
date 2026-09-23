---
id: TASK-134
title: 'Work queue S8: evidence-driven additions intake'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - Blocked
milestone: m-1
dependencies: []
documentation:
  - docs/work-queue-tui-proposal.md
priority: low
type: spike
ordinal: 46000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan section 16 (S8) lists additions that each require their own decision backed by evidence: more providers (Beads, Linear, Jira, or GitLab, by demand), a native-authority study under the plan section 9 admission criteria, and a separately authenticated source service once duplicated traffic, latency, and authorization needs are measured. None of them is justified yet. This task is the intake gate: it gathers evidence and turns each accepted addition into its own task, instead of building anything speculatively.

Blocked until evidence exists: a concrete request for a named provider, a recorded measurement of duplicated source traffic, latency, or authorization need, or a native-authority requirement. Remove the Blocked label only when citing that evidence. A native-authority study or source-service decision does not wait for S7. Any accepted addition implemented as an external adapter must depend on the S7 parent task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 For each candidate (Beads, Linear, Jira, GitLab, a native-authority study, a source service), evidence of demand or measured need is recorded, or its absence is stated
- [ ] #2 Each candidate's decision (accept, defer, or reject, with rationale) is recorded in plan sections 2 and 16 before any task is created for it
- [ ] #3 Each accepted candidate gets its own backlog task with acceptance criteria, linked from this task. External-adapter implementations depend on the S7 parent task
- [ ] #4 The task is not completed with every candidate deferred for lack of evidence; it stays open and Blocked instead
- [ ] #5 A native-authority study, if accepted, is scoped against every plan section 9 admission criterion
- [ ] #6 A source service, if accepted, cites the measured traffic, latency, and authorization evidence that justifies it
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run doc-test` passes and every changed relative link resolves
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
