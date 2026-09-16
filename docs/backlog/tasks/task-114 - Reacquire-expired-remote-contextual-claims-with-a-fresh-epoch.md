---
id: TASK-114
title: Reacquire expired remote contextual claims with a fresh epoch
status: Done
assignee:
  - '@pi'
created_date: '2026-09-16 17:50'
updated_date: '2026-09-16 18:59'
labels: []
dependencies: []
references:
  - internal/cli/remote_lifecycle.go
  - internal/cli/lease_commands.go
  - docs/claim-model.md
  - internal/authority/authority.go
  - internal/authority/handle_pending.go
  - internal/authority/time.go
  - internal/cli/remote_commands_test.go
  - internal/authority/regression_test.go
  - docs/cli-reference.md
modified_files:
  - docs/claim-model.md
  - docs/cli-reference.md
  - internal/authority/authority.go
  - internal/authority/handle_pending.go
  - internal/authority/http.go
  - internal/authority/regression_test.go
  - internal/cli/remote_commands_test.go
  - internal/cli/remote_lifecycle.go
  - internal/handle/handle.go
  - internal/lease/service.go
priority: high
type: bug
ordinal: 156000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
With the remote profile selected, an expired contextual handle is reused by acquire instead of being replaced with a new ownership epoch. The reproduced default-handle flow listed coordination:demo as expired, then reused claim ef6473a06db7d017b9ee21230030faeb and its completed acquire operation rather than generating a fresh claim. Users should be able to reacquire an expired resource in the same checkout without inventing a new session or understanding handle internals. The local acquire path already distinguishes active, expired, and pending handles; the remote path must provide equivalent safe lifecycle behavior while preserving uncertain-request recovery.

## Implementation context
- Start in remoteAcquire (internal/cli/remote_lifecycle.go): every readable non-active handle currently contributes its old claim ID, token, agent, session, and resources. Expiry is checked against time.Now(), not sufficient evidence of remote inactivity. Reusing these values can replay the completed acquire instead of creating a new epoch.
- Compare the local acquire path in internal/cli/lease_commands.go: an apparently expired ready handle triggers Status by claim ID, refuses a still-active claim, and resets epoch identity for an inactive one. This is a behavioral reference, not a request to route remote operations through the local store.
- Follow RemoteAuthority.Acquire in internal/authority/authority.go and persistHandleRequest/activateGrantHandle in internal/authority/handle_pending.go. Merely generating new IDs in the CLI is insufficient: persistence currently rejects a different claim ID in an existing handle. Use the existing owner-private, locked handle primitives; revalidate the old epoch/pending state under lock before replacement so a concurrent renewal or pending write cannot be overwritten.

## Required state transitions and boundaries
- Missing handle: retain normal fresh acquisition behavior. Ready, definitively inactive remote claim: generate a new claim ID/token, use current acquisition inputs, and stage the new exact request durably before dispatch. Only a validated grant may promote that pending handle to ready. The original wording about replacement after confirmation means ready-state publication, not permission to skip write-ahead persistence.
- Ready but remotely active, status unavailable/ambiguous, or authority mismatch: fail closed without replacing ownership. Client wall-clock expiry alone does not authorize replacement; use the existing authority status/time contract.
- Any unresolved pending/recovery request takes precedence over apparent expiry. Preserve its bytes, operation identity, credentials, deadline, and trust binding. Recover an exact pending acquire through the existing recovery path; never silently replay it as a fresh acquisition or discard a different pending operation.
- A malformed/unreadable/unsafe handle is not a missing handle. Preserve it and return the existing classified error rather than silently overwriting it.
- No new session flag, protocol, store schema, local-authority fallback, or manual handle deletion workaround. Generated SessionID is epoch metadata; an explicitly selected session remains the contextual lookup selector.

## Coordination
No prerequisite task. TASK-115 owns recovery error classification/hints and TASK-116 owns text rendering. TASK-114 must be safe independently; coordinate edits to shared CLI/authority files rather than introducing a dependency solely for file overlap.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 With a selected remote profile and a definitively inactive ready contextual handle, acquire creates a different claim ID and token at the same selected path without requiring --session or --handle. The new exact request is durably pending before dispatch and becomes ready only after a validated grant; the completed old acquire is not returned as fresh success.
- [x] #2 An active remote claim, ambiguous/failed status lookup, authority mismatch, or unresolved pending/recovery request prevents fresh-epoch replacement. Pending request bytes, identity, deadline, and credentials remain recoverable; client wall-clock expiry alone is insufficient.
- [x] #3 A fresh epoch uses the current resource set, agent/session selection, work key, TTL, coordination mode, and request deadline rather than stale epoch metadata. Default selection remains stable; an explicit session selects an independent contextual handle.
- [x] #4 Remote CLI/adapter regression tests cover singleton and multi-resource expiry, changed acquisition inputs, stale local expiry with remote activity, status failure, pending acquire and non-acquire preservation, default and explicit session selection, and concurrent handle change. A lost response leaves the new pending request recoverable and exact replay yields only that new epoch.
- [x] #5 docs/claim-model.md and the relevant CLI reference explain ready versus pending replacement, generated claim session metadata versus the optional session selector, and why changing session is not recovery for an uncertain request.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 Add regression tests that fail before the fix and pass afterward; record the commands and evidence in this task without claiming unexecuted scenarios.
- [x] #2 Run mise run lint, mise run format-check, mise run test, and mise run typecheck; stage intended changes and run mise run hooks before committing.
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a failing remote CLI integration test using the existing remoteCLIFixture fixture in internal/cli/remote_commands_test.go; reproduce expired acquire returning the old claim. Add focused authority persistence tests as needed.
2. Implement the ready/inactive versus pending decision and a locked, durable new-epoch transition across remoteAcquire and the existing authority handle persistence layer. Keep network uncertainty recoverable and avoid nested acquisition of the same handle lock.
3. Exercise the acceptance matrix, including concurrent-change rejection and lost-response replay; assert claim IDs, persisted token differences (never print tokens), request bytes, and dispatched request counts.
4. Update contextual-handle documentation and run the required repository gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented remote ready-handle status validation, lock-bound fresh-epoch staging, exact pending acquire replay, grant activation metadata refresh, regression coverage for expired reacquire/current inputs, stale local expiry with remote activity, lost-response replay, and concurrent handle changes. Focused checks: go test ./internal/authority ./internal/lease; go test ./internal/cli -run 'TestRemoteCLI'.

Review found and fixed three boundary defects: authoritative inactivity can replace a locally future-dated ready handle through exact locked revalidation; nested status claim identity is validated; mismatched grant metadata leaves the new request pending. Independent verifier passed all criteria. Required gates passed: mise run lint, format-check, test, typecheck, and hooks under an isolated HOME using the existing mise installation. Post-merge focused packages also passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Remote acquire now status-checks ready contextual claims, stages a lock-bound fresh epoch only for authoritative inactivity, replays pending requests exactly, validates nested status and grant identity, and preserves uncertain outcomes. Added lost-response, concurrent-change, stale-local-expiry, current-input, default/explicit-session, and identity regression coverage plus docs. Committed and fast-forwarded as ab4f23b; all required gates and post-merge focused tests passed.
<!-- SECTION:FINAL_SUMMARY:END -->
