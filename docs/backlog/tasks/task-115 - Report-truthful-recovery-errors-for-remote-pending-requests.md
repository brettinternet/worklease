---
id: TASK-115
title: Report truthful recovery errors for remote pending requests
status: To Do
assignee: []
created_date: '2026-09-16 17:51'
updated_date: '2026-09-16 18:02'
labels: []
dependencies: []
references:
  - internal/authority/http.go
  - internal/cli/remote_lifecycle.go
  - internal/cli/lease_commands.go
  - docs/remote-claim-authority.md
  - internal/authority/pending.go
  - internal/authority/handle_pending.go
  - internal/authority/regression_test.go
  - internal/cli/remote_commands_test.go
  - internal/reason/reason.go
priority: high
type: bug
ordinal: 157000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Remote mutation preparation can detect a pending-request collision before any network dispatch, but the client currently collapses the underlying failure into storage-failure with commitState unknown. In the reproduced expired-claim flow, heartbeat reported operation-request-mismatch and acquire reported “secondary remote request could not be durably recorded,” leaving no actionable distinction between a changed request, a local persistence failure, and a genuinely uncertain remote effect. Recovery output must preserve the safest specific cause, state whether dispatch occurred, and tell the operator which exact action is permitted next.

## Implementation context
- HTTPClient.Call in internal/authority/http.go stages a named request through persistHandleRequest before c.do. A different unresolved handle request returns errHandlePending; some operations then use PendingStore.Save as a secondary durable slot. Its classified collision/persistence errors are currently replaced with generic storage-failure.
- Inspect internal/authority/pending.go for the stable request-mismatch and storage errors; preserve safe registered reasons rather than exposing raw filesystem errors or request payloads. Cover primary handle staging, secondary pending-store staging, and standalone pending-store staging consistently.
- mutationFailure in internal/cli/lease_commands.go derives unknown for reasons outside DefinitiveNoCommit unless commitState is already present. Set evidence-based state at the pre-dispatch boundary and preserve it through CLI wrapping. Do not globally declare storage-failure definitive: storage can fail after a remote effect.
- Heartbeat, checkpoint, and release use common lifecycle preparation/operation IDs; remoteTransfer already rejects unrelated pending kinds. Detect a pending acquire before constructing/dispatching an unrelated mutation and report the pending operation that actually needs recovery.

## Error/recovery contract
- commitState describes the attempted request, not every operation retained in the handle. A newly blocked attempt can be not-committed while the older pending request remains unknown. If the same request identity was previously dispatched, a failed retry staging attempt does not prove that request never committed; retain unknown unless existing evidence proves otherwise.
- Prove pre-dispatch classification with a counting transport: zero mutation dispatch for the blocked attempt. Preserve pending bytes, original deadline, operation ID, credential references, and authority/restore/certificate binding. Do not clear older state because a new attempt failed locally.
- After possible dispatch, retain unknown and the exact pending request unless a validated authoritative response establishes the result. Definitive rejection retains its registered reason; successful exact replay clears pending only after typed result validation and handle activation.
- Recovery hints must name an existing supported action, the correct pending identity/path, and its limits. Use the existing lifecycle retry entry points and HTTPClient.ReplayHandle/Replay primitives; there is no standalone CLI pending replay command today, so do not invent one in a hint. Verify that the suggested lifecycle retry actually replays the retained request, wiring the narrow recovery path if necessary; do not tell users to change request inputs, extend a deadline, delete a handle, or use a new session to evade uncertainty. Fresh acquire is appropriate only after definitive inactivity with no unresolved request. --session is only for a distinct independent loop.
- Reuse the existing error envelope and safe detail fields (commitState, pendingPath, operationId, recoveryHint); no new protocol/reason taxonomy or generic recovery framework is required. Do not expose rawRequest, tokens, installation credentials, or private payloads in text, JSON, or hints.

## Coordination
Independently deliverable from TASK-114 (epoch replacement) and TASK-116 (text path rendering). Keep classification/dispatch tests separate from the known text digest-redaction defect; TASK-116 supplies exact path-rendering coverage. Shared source files require integration care, not a dependency.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A newly attempted mutation that fails durable staging before any dispatch reports commitState not-committed, preserves an existing safe classified cause (including operation-request-mismatch), and uses storage-failure only for actual unclassified persistence failures. Tests prove zero mutation dispatch and unchanged older pending state; a previously dispatched identical request is not falsely declared uncommitted.
- [ ] #2 Failures after possible dispatch retain commitState unknown and the exact durable pending request unless validated authoritative evidence establishes otherwise. CLI mutationFailure preserves an explicitly established state; storage-failure is not globally reclassified as definitive.
- [ ] #3 Heartbeat, checkpoint, release, and transfer encountering a pending acquire fail before dispatch with a recovery-required explanation and the pending acquire identity/path, rather than an unexplained mismatch for the newly attempted lifecycle action. Exact pending acquire recovery remains available.
- [ ] #4 Bounded recovery hints identify an existing executable exact-replay action and distinguish the attempted request from older pending uncertainty. Fresh acquire is suggested only for definitive inactivity without unresolved pending work; --session is described only as an independent-loop selector, never an uncertainty bypass.
- [ ] #5 Text and JSON tests cover staging persistence failure, secondary request collision, previously dispatched retry, definitive remote rejection, uncertain transport, pending-acquire lifecycle refusal, and validated exact replay. Assert stable reason/state, dispatch count, pending preservation/cleanup, actionable hints, and absence of credentials/private payloads.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Add regression tests that fail before the fix and pass afterward; record the commands and evidence in this task without claiming unexecuted scenarios.
- [ ] #2 Run mise run lint, mise run format-check, mise run test, and mise run typecheck; stage intended changes and run mise run hooks before committing.
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add focused counting-transport tests around HTTPClient.Call for each staging route and same-ID retry, then reproduce pending-acquire lifecycle failures through the remote CLI fixture.
2. Preserve safe underlying classified causes and attach evidence-based commitState where dispatch is known; audit downstream wrapping and pending cleanup without weakening post-dispatch uncertainty.
3. Add narrow pending-acquire lifecycle guards and bounded hints using the existing recovery CLI. Verify the referenced commands and retain replay trust/identity/deadline checks.
4. Update the recovery section of docs/remote-claim-authority.md with the attempted-versus-pending distinction and supported next actions; run focused and repository gates.
<!-- SECTION:PLAN:END -->
