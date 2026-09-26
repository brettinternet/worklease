---
id: TASK-133.2
title: Host external adapters as supervised processes
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 22:55'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies:
  - TASK-133.1
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-133
priority: medium
type: feature
ordinal: 44000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The queue must run an external adapter as one supervised, long-lived process per source, never one per item (plan section 11). A crash isolates its own source. A crash after dispatching a write leaves the mutation uncertain and routes it into S6 recovery. Installation needs explicit executable and version selection with user approval, and nothing is ever downloaded or run automatically.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 queue.yaml can declare an external adapter by explicit executable path and expected version, and the queue refuses to run it until the user approves it through an explicit command that records approval (for example, by executable digest)
- [x] #2 The host speaks the TASK-133.1 protocol with request IDs, cancellation, deadlines, size bounds, and backpressure, and it restarts a crashed adapter with bounded backoff
- [x] #3 A crash, hang, or malformed message affects only that source, which shows a diagnostic. A crash after dispatching a write marks the operation unknown in the S6 journal
- [x] #4 The adapter process gets a minimized environment and only source-scoped credential references. Worklease bearer credentials never reach it, as tested with canaries
- [x] #5 stderr is bounded, redacted, and surfaced as diagnostics
- [x] #6 Tests use a scripted fake adapter binary to cover crash, hang, malformed output, oversized messages, cancellation, and approval refusal
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Map the v1 protocol to the existing queue configuration, source adapter, and S6 write journal. 2. Add explicit digest approval and a source-scoped supervised process with bounded JSON-RPC transport and safe environment. 3. Integrate source diagnostics and unknown write recovery; test failure, cancellation, limits, approval, and credential isolation. 4. Run gates and review once; commit, integrate, finalize and release.

5. Address review-confirmed process lifecycle, approval binding, restart, pagination and diagnostic defects; rerun affected tests and gates before delivery.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Config foundation in isolated task-133-2-adapter-host worktree: queue.yaml external binding plus owner-private executable SHA-256 approval/check and focused tests. Transport, source integration, CLI approval and write recovery remain. No commit yet.

Added source-scoped supervised JSON-RPC transport and scripted process tests in task worktree. Focused queue race tests pass; read adapter, CLI registration, approval command, and provider write mapping remain. Host currently limits external resource policy to generic pending explicit binding support.

Read-side external source adapter and queue CLI approval/query integration now implemented in worktree with focused tests. Exact generic claim binding is optional for read-only sources, mandatory for claims, and included in executable approval; change forces reapproval. Write adapter/journal integration, comprehensive gates and delivery remain.

S6 external WriteAdapter added with operation-ID-scoped authorization, readReceipt and journal unknown-on-crash tests. CLI write/recovery integration underway. Lint fix applied in transport; full gates pending.

One risk-focused review found actionable approval/exec race, mutable source binding, process descendants, unanswered cancellation slots, restart resolve, pagination generation, and suppressed stderr diagnostic paths. Non-generic host policy is an explicit v1 restriction in proposal, not an unintended compatibility promise. Corrections in progress.

Verified on main after merge: mise run lint, format-check, test, typecheck, doc-test; staged pre-commit mise run hooks passed. Focused new/changed tests passed go test -race -count=3 for config/queue/cli. Scripted fake process covers approval refusal, crash/restart, cancellation/hang, malformed/oversized output, source-scoped environment canaries, stderr redaction, and S6 unknown crash recovery. One general review found seven concrete defects, all fixed and rerun; generic-only external claim policy is documented host restriction. Implementation 8c380c9; merged to main 877828b. Next step: TASK-133.3 can build conformance suite against this host.

Post-completion review (6800074): launch snapshots moved under owner-private config dir (not TMPDIR); writes pinned to the validated process generation; idle crash stderr surfaced on next list; rate-limited retryAt gates the source; empty item IDs rejected; source-scope check limited to wire ref fields; credential variants deduped and bounded; configSchema accepts format/examples. Regression tests added. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Hosted approved external queue adapters with bounded supervised JSON-RPC, isolated diagnostics, generic claim binding, and journaled write recovery. Verified full gates, focused race tests, scripted process failures and one resolved review; committed 8c380c9 and merged 877828b.
<!-- SECTION:FINAL_SUMMARY:END -->
