---
id: TASK-150
title: Renew and release owned claims from the Claims TUI
status: Done
assignee: []
created_date: '2026-09-27 01:08'
updated_date: '2026-09-27 16:40'
labels:
  - reviewed
dependencies: []
references:
  - internal/queueui/claims_keys.go
  - internal/cli/queue_claims.go
  - internal/cli/lease_commands.go
  - >-
    docs/backlog/tasks/task-145 -
    Add-an-authority-wide-Claims-tab-to-the-queue-TUI.md
priority: medium
type: feature
ordinal: 90000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The authority-wide Claims tab (TASK-145) is read-only. A user who sees a claim nearing expiry, or one they have finished, must leave the TUI and find its private handle to renew or release it. Add renew and release for claims whose private handle is discoverable under the current Worklease home, in both configured-queue and claims-only sessions. Public holder identity (agent/session) is never proof of ownership; only the private handle is.

Eligible handles are the contextual (`<home>/handles/ctx-*.json`) and MCP (`<home>/handles/mcp-*.json`) handles whose authority ID, claim ID, and (for remote) restore ID match the selected authority and the selected claim. Everything else shows why the actions are unavailable:
- Queue-owned handles (`<home>/queue-handles/...`): managed by the queue item lifecycle (auto-renew, provider-aware release/cancel). Point to `i` / `worklease queue`; do not mutate from Claims.
- Handles referenced by a live `worklease run` supervisor record: point to `worklease runs stop`; the supervisor owns heartbeat and release.
- Handles in pending or recovery state: show the exact CLI recovery command (`worklease heartbeat|release --handle PATH`); the TUI does not resume or reconcile pending operations.
- Handles at arbitrary `--handle`/`--path` locations or explicit token files: not discoverable; show the CLI command form.

Action scope decision: renew and release only. Acquire stays in the queue item flow and CLI (ad-hoc resource acquisition is not a browsing task); checkpoint needs free-form progress input better served by agents and the CLI; transfer needs successor identity and handle hand-off; there is no authority-side revoke for non-owned claims, which can only expire. Renew uses the default TTL exactly like `worklease heartbeat` without `--ttl`; no TTL input.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Claims list rows mark claims with an eligible private handle, and the detail shows either the handle kind and path or the specific reason actions are unavailable (not held here, queue-owned, supervised run, pending/recovery, authority or restore mismatch). Handle discovery reads local files only, never sends private credentials during browsing, and never treats matching agent/session identity as ownership.
- [x] #2 `u` renews and `R` releases the selected eligible active claim (matching `R` in the item view); both are listed in help. Each opens a preview showing authority, claim ID, resources, handle path, current expiry, and effect (renew: default TTL and projected expiry; release: resources become free and the handle is removed). Release requires a reason, default "released", recorded in history. Only explicit confirmation dispatches; navigation, refresh, filter changes, and dismissal never mutate.
- [x] #3 Immediately before dispatch, under the handle lock for local authorities, re-read the handle and refuse if its claim ID, revision, authority, restore ID, or ready state changed, if the claim is no longer active or has expired on the authority clock, if the selected authority changed, or if the handle is now queue-owned or supervised. A refusal mutates nothing and names the cause.
- [x] #4 Renew and release share the CLI heartbeat/release implementation (pending-request persistence, operation IDs, definitive-no-commit clearing, remote revision persistence, handle removal after release) rather than a parallel copy. Ambiguous outcomes leave the handle pending, report "outcome uncertain" with the exact CLI recovery command, and never report success or delete the handle.
- [x] #5 After a definitive result the list and detail show the new expiry or the claim removed without waiting for the next poll; selection stays on the claim after renew and moves to the neighboring row after release. Only one Claims mutation runs at a time, and quitting during one waits or warns as other TUI mutations do.
- [x] #6 docs/queue-tui.md documents the keys, eligibility rules, and unavailable reasons; `worklease queue claims` usage no longer calls the tab read-only.
- [x] #7 Focused tests cover: eligible contextual and MCP handles; non-owned claims with matching agent identity; queue-owned, supervised, and pending handles refused; local and remote authorities including restore-ID mismatch; claims-only startup; confirmation dismissal; claim, revision, and authority drift between preview and dispatch; an ambiguous outcome retaining the pending handle; list/detail update after success.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extract the handle-level heartbeat and release cores from internal/cli/lease_commands.go into functions taking the backend, handle path, lock, and inputs, so the CLI actions and the TUI call the same code; keep CLI behavior and tests unchanged.
2. Add a claims handle index in internal/cli: scan the home handles directory for ctx-/mcp- handles, read metadata, and classify each against the selected authority, the queue-handles directory, and live run records; rescan on each Claims refresh.
3. Extend queueui.ClaimsState with the index, preview state, and Renew/Release callbacks returning result messages; add `u`/`R` keys, preview/confirm/reason input modes, the row marker, a detail eligibility section, and help text.
4. Wire the callbacks in configureClaimsTab so queue and claims-only sessions both get them; dispatch revalidates under the lock, then calls the shared cores.
5. Apply results to Claims.Items immediately, then trigger a refresh.
6. Add tests per the last AC at the queueui layer (keys, preview, drift display) and cli layer (index classification, dispatch revalidation, local and remote via testkit); update docs/queue-tui.md and the command usage.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Started in isolated Worktrunk checkout; validating existing lease cores and Claims UI before implementation.

CLI handle-index and lock/revalidation path implemented in task-150-claims-cli; extracted shared heartbeat/release handle cores and added local/remote/drift/uncertain-outcome tests. Queue UI implementation is running in separate task-150-claims-actions checkout; integration and checks pending.

Verified: Claims UI preview/confirmation, dismissals, busy quit, immediate renew/release and stale-poll suppression in queueui tests; CLI index and dispatch tests cover contextual/MCP, missing holder identity, queue/run/pending refusal, local/remote restore identity, authority/claim/revision drift, ambiguous retained pending and exact custom-reason replay. An actual claims-only PTY session renewed and released through a writable authority; lifecycle event history retained the release reason. One independent review found three concrete defects (claims-only read-only backend, in-flight poll rollback, recovery command precision), all fixed and retested. Focused race -count=3, full lint/format-check/typecheck/test/race on merged main, doc-test, and staged hooks passed. Implementation 8fe27f5, merge 296220f, history assertion 3905cae; both session-owned worktrees and branches removed. No remaining blocker.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added private-handle renew/release to the Claims TUI with guarded confirmation, shared CLI lifecycle, immediate updates and recovery guidance. Verified local/remote and claims-only behavior, full quality gates, review fixes and lifecycle history; merged on main (296220f, 3905cae) and cleaned up worktrees.
<!-- SECTION:FINAL_SUMMARY:END -->
