---
id: TASK-133.3
title: 'Build the adapter conformance suite, sample adapter, and authoring guide'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 22:55'
labels:
  - work-queue
  - reviewed
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
- [x] #1 A conformance suite covers pagination, stale writes, capability denials, cancellation, quota handling, malformed output, uncertain outcomes, identity vectors, and secret leakage
- [x] #2 The Backlog.md and GitHub built-in adapters pass the suite through a shim that runs them behind the protocol boundary
- [x] #3 A sample external adapter (for example, over a static JSON file) lives in the repository, passes the suite, and runs under the TASK-133.2 host
- [x] #4 An authoring guide under skills/worklease-workflow/references/ explains the manifest, protocol, resource policy selection, trust model, and how to run the suite, and it links the source-provider authoring checklist
- [x] #5 The suite runs in `mise run test`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a shared protocol-facing conformance harness with fixtures and identity vectors; exercise sample and built-in adapters through narrow shims.
2. Ship a deterministic external adapter executable and document approval, manifest, wire behavior, resource policy, trust, and suite usage.
3. Run focused race tests and all project gates, review item-scoped risks, commit in the worktree, merge to main, record evidence, and release claim.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented shared host-backed conformance tests for Backlog.md, GitHub, and static sample; added identity vectors, cancellation and stale-write readback cases. Sample and guide are committed on task-133-3-conformance (b7d6ffe) and merged into task-133-3-suite. Focused race checks pass; full gates and final integration pending.

Evidence: on main c68c22b, TestAdapterConformance exercises all three via ExternalAdapter/ExternalProcess; TestAdapterConformanceBuiltInCancellation reaches both built-in providers; stale writer preflight/readback, quota, malformed output, unknown outcomes, secret redaction and shared identity vectors run under the conformance prefix. Sample host test passes; guide links checklist. go test -race -count=3 for TestAdapterConformance* (internal/queue) and TestSampleAdapter* (internal/sampleadapter) passed. mise run lint, format-check, typecheck, test, hooks passed on the suite head merged unchanged to main; focused conformance tests passed again on main. One general review found three actionable gaps (pagination bound, stale-write shim, built-in cancellation), all fixed and retested. Proposal section 11/D16 already matches the implementation; no design amendment needed.

Delivery commits: b7d6ffe (sample), c68c22b (suite integrated on main), bfcbde1 (task finalization). No remaining blocker. Next independent step: TASK-133 parent integration checklist; verify its criteria on main before closing it. Both session-owned worktrees and their Herdr workspaces were removed.

Post-completion review (6800074): sample adapter cursors bound to filters/ref; stale-write shim compares authoritative state and append evidence before/after; shim preserves GitHub rate-limited diagnostic and retryAt; cancellation check rejects premature .done. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added a shared supervised-protocol conformance suite for built-in and sample adapters, static sample executable and authoring guide; validated race tests and all project gates, merged to main at c68c22b.
<!-- SECTION:FINAL_SUMMARY:END -->
