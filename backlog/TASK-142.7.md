---
id: TASK-142.7
title: Add recoverable focused Linear writes and Start work
status: done
type: feature
priority: medium
labels: [work-queue]
milestone: m-1
parent: TASK-142
blocked_by: [TASK-142.1, TASK-142.2, TASK-142.3, TASK-142.4, TASK-142.5, TASK-142.6]
rank: a8072f9b658072f2a
refs: [docs/work-queue-tui-proposal.md]
created: 2026-09-25T16:30:00Z
updated: 2026-09-25T22:21:00Z
---

Linear provider mutations are separate from Worklease claims. Ship only after probe, read, sync, and claim behavior is established; never infer write safety from a displayed workflow state.

## Acceptance criteria

- [x] Configured state transitions including Start work, comments carrying a verifiable operation marker, and assign-to-me follow §8 intent/dispatch/receipt/read-back/checkpoint recovery
- [x] Verify viewer against configured account before every write and on credential changes; a different current assignee requires explicit confirmation of single-assignee replacement
- [x] Lost responses and lagging read-back never cause an unsafe redispatch; tests cover principal mismatch, partial effects and ambiguous markers

## Plan

1. Add an explicitly configured Linear write adapter using the existing write pipeline: fresh viewer and scope checks, state/assignment/comment dispatch through the quota scheduler, exact receipt read-back and marker/provenance verification.
2. Wire capabilities, preview, Start work, recovery, and CLI controller to the adapter; preserve explicit assignee-replacement confirmation and reject unsupported transitions.
3. Add focused in-process GraphQL fixtures for principal mismatch, lost responses, lagging read-back, partial effects and ambiguous markers; run targeted race tests and repository gates.
4. Review, commit the isolated change, merge to main, record acceptance evidence and clean the owned worktree.

## Notes

Implemented Linear state/Start, marked comments and single-slot assignment through the journaled write pipeline; read-back verifies exact state/assignee or unique marker+content+actor. Fresh viewer identity is checked immediately before dispatch. Recovery validates journaled source generation, including checkpoint-pending and CLI retry. Review found invalid issueUpdate argument placement, progress preview append kind, and recovery binding drift; all corrected. An MCP unknown-transition test raced its 30s unrenewed claim under load; it now enables normal heartbeat during provider verification. No live Linear writes were made. Verification: go test -race -count=3 -run TestLinear ./internal/queue ./internal/cli; focused Linear config race test; mise run lint, format-check, test, typecheck, and staged hooks all passed after rebase. Code commit 15d0f9d fast-forward merged to main. Known limits: state/assignment lost responses without attributable provider receipt remain unresolved; quotas coordinate per client process.

## Summary

Added recoverable, account-verified Linear state/Start, marked-comment and confirmed assignment writes. Focused race tests, full checks, staged hooks and scoped review passed; merged 15d0f9d to main.
