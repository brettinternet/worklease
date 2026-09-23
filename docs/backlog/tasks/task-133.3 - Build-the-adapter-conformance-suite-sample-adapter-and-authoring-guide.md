---
id: TASK-133.3
title: 'Build the adapter conformance suite, sample adapter, and authoring guide'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-133.1
  - TASK-133.2
references:
  - skills/worklease-workflow/references/source-provider-authoring-checklist.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-133
priority: medium
type: feature
ordinal: 45000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
One conformance suite must run against both the built-in adapters and external ones (plan section 11), so external authors meet the same bar the built-ins do. The suite needs a fake provider, golden fixtures, and identity vectors, plus tests for pagination, stale writes, capability denials, cancellation, quota handling, malformed output, uncertain outcomes, and secret leakage. A sample adapter and an authoring guide make the protocol usable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A conformance suite covers pagination, stale writes, capability denials, cancellation, quota handling, malformed output, uncertain outcomes, identity vectors, and secret leakage
- [ ] #2 The Backlog.md and GitHub built-in adapters pass the suite through a shim that runs them behind the protocol boundary
- [ ] #3 A sample external adapter (for example, over a static JSON file) lives in the repository, passes the suite, and runs under the TASK-133.2 host
- [ ] #4 An authoring guide under skills/worklease-workflow/references/ explains the manifest, protocol, resource policy selection, trust model, and how to run the suite, and it links the source-provider authoring checklist
- [ ] #5 The suite runs in `mise run test`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
