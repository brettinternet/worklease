---
id: TASK-133
title: 'Work queue S7: external adapter protocol'
status: done
type: feature
priority: medium
assignees: ['@pi']
labels: [work-queue, reviewed]
milestone: m-1
blocked_by: [TASK-133.1, TASK-133.2, TASK-133.3]
rank: a4b73dfa9c4b73db8
refs: [docs/work-queue-tui-proposal.md]
created: 2026-09-23T04:29:00Z
updated: 2026-09-25T23:07:00Z
---

Adding sources beyond the two built-ins should not require changing Worklease. D16 defers an external adapter protocol until both built-ins have exercised the model. That avoids repeating the Python era's mistake of shipping a source SDK before any real adapter existed (plan section 3, "Prior art"). The protocol is JSON-RPC 2.0 over stdio with newline-delimited messages and a Worklease-specific method set that mirrors the section 7 operations. Resource policies stay static built-ins; an external adapter selects an existing policy. See plan sections 11 and 16 (S7).

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.

## Acceptance criteria

- [x] Every S7 child task is Done
- [x] Both built-in adapters pass the shared conformance suite
- [x] Tests cover adapter crashes, malformed output, cancellation, and secret redaction
- [x] Installing an external adapter requires explicit user approval

## Plan

Recheck completed S7 children and run the shared conformance and failure/approval tests on the current main tree; record criterion-specific evidence, then close the integration checklist.

## Notes

Main dc9ca80: TASK-133.1/.2/.3 are Done. On matching worktree HEAD, go test -count=1 -run TestAdapterConformance ./internal/queue passed (backlog-md, github, sample shims). The same focused run exercised TestAdapterConformanceUncertainMutationAfterCrash, RejectsMalformedAndOversizedResponses, CancellationNotifiesAdapter, CancellationGraceRestartsAndReclaimsSlots, SecretRedaction, and ExternalProcessApprovalRefusalDoesNotExecute; approval/config and CLI focused tests passed. TestQueueAdapterApprovalRequiresExplicitSourceAndConfirmation and config approval tests demonstrate explicit approval. mise run lint, format-check, test, typecheck all passed. No implementation changes; integration checklist only.

Delivery: commit 7a3c90a (provider integration checklist), fast-forward merged to main 7a3c90a; no implementation code changes. One proportional review pass: focused criterion checks and full gates found no item-scoped defect. No blocker or further S7 step.

Post-completion review: all S7 children Done and reviewed; TestAdapterConformance (backlog-md, github, sample) re-passed on main-equivalent HEAD. No follow-up.

## Summary

S7 integration checklist verified on main-equivalent HEAD: all children Done, conformance across built-ins and sample plus process failure/approval checks passed; full repository gates passed.
