---
id: TASK-133
title: 'Work queue S7: external adapter protocol'
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 06:14'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-133.1
  - TASK-133.2
  - TASK-133.3
documentation:
  - docs/work-queue-tui-proposal.md
priority: medium
type: feature
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Adding sources beyond the two built-ins should not require changing Worklease. D16 defers an external adapter protocol until both built-ins have exercised the model. That avoids repeating the Python era's mistake of shipping a source SDK before any real adapter existed (plan section 3, "Prior art"). The protocol is JSON-RPC 2.0 over stdio with newline-delimited messages and a Worklease-specific method set that mirrors the section 7 operations. Resource policies stay static built-ins; an external adapter selects an existing policy. See plan sections 11 and 16 (S7).

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every S7 child task is Done
- [x] #2 Both built-in adapters pass the shared conformance suite
- [x] #3 Tests cover adapter crashes, malformed output, cancellation, and secret redaction
- [x] #4 Installing an external adapter requires explicit user approval
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Recheck completed S7 children and run the shared conformance and failure/approval tests on the current main tree; record criterion-specific evidence, then close the integration checklist.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Main dc9ca80: TASK-133.1/.2/.3 are Done. On matching worktree HEAD, go test -count=1 -run TestAdapterConformance ./internal/queue passed (backlog-md, github, sample shims). The same focused run exercised TestAdapterConformanceUncertainMutationAfterCrash, RejectsMalformedAndOversizedResponses, CancellationNotifiesAdapter, CancellationGraceRestartsAndReclaimsSlots, SecretRedaction, and ExternalProcessApprovalRefusalDoesNotExecute; approval/config and CLI focused tests passed. TestQueueAdapterApprovalRequiresExplicitSourceAndConfirmation and config approval tests demonstrate explicit approval. mise run lint, format-check, test, typecheck all passed. No implementation changes; integration checklist only.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
S7 integration checklist verified on main-equivalent HEAD: all children Done, conformance across built-ins and sample plus process failure/approval checks passed; full repository gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
