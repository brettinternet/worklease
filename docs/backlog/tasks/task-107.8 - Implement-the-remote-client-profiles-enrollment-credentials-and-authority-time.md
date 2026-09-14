---
id: TASK-107.8
title: >-
  Implement the remote client: profiles, enrollment, credentials, and authority
  time
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 08:06'
labels:
  - remote-authority
dependencies:
  - TASK-107.1
references:
  - internal/config
  - internal/handle
  - internal/cli
  - TASK-85.10
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 140000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement one narrow shared authority interface for the actual CLI, MCP, and guard consumers, with local and remote implementations. The remote implementation owns trusted profiles, explicit profile selection, enrollment, credentials, immutable request envelopes, authority-time bounds, exact pending recovery, and fail-closed transport. When a remote profile is selected, consumers do not open client SQLite for authority state. The existing local service continues to use SQLite directly through the same consumer-facing interface. Do not add a backend registry. Contract fakes allow CLI and MCP work before a live server exists.

Every built-in remote mutation durably saves the exact request before dispatch, including enrollment, administration, and `--no-handle`. Claim operations reuse handle pending state. A guarded operation retains its original start and effect evidence until the operation is terminal or reconciled; a successful begin or renewal response does not make the parent effect terminal. Renewal, completion, reconciliation, and other requests use bounded request-scoped profile recovery records when the handle pending slot is occupied, so their recovery cannot overwrite or erase the original unresolved effect. Requests without a named handle also use bounded request-scoped profile recovery records. A credential descriptor supplies a secret but is not durable recovery storage. Pending uncertainty clears only after a confirmed terminal response for that request, reconciliation, or definitive proof that the original dispatch did not commit. Authentication, revocation, incarnation errors, timeout, age, GC, replay expiry, profile refresh, and credential rotation do not clear or rewrite it. Local `--no-handle` behavior remains unchanged.

Profiles bind a trusted endpoint, expected authority, and pinned restore incarnation. The precedence is `--profile`, `WORKLEASE_PROFILE`, user-side project binding, user default, then local. Repository content cannot select or redirect a profile, and a configured remote profile never falls back to local state. The client uses fresh authority-time samples to schedule renewals at half TTL, stop new guarded work at three quarters, and compute `requestNotAfter` from the lower bound plus 24 hours. OS credential-store integration follows contract D2 and requires an amendment before adding any new runtime module; no platform-command implementation is mandated.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 One narrow authority interface serves the actual CLI, MCP, and guard consumers through the existing local SQLite service or the remote client. Remote-selected consumers do not open local SQLite authority state, local mode retains its current SQLite behavior, no backend registry is added, and contract fakes work without a live server.
- [ ] #2 Profile selection follows the fixed precedence, repository content cannot select or redirect a profile, and an unbound invocation with no explicit profile, environment selection, or default uses local state without network access.
- [ ] #3 Enrollment uses bounded public metadata discovery, saves immutable authority and restore identity plus exact pending state, generates and durably stores the installation credential before dispatch, replays a dropped response exactly, and activates the profile only after confirmed or recovered success.
- [ ] #4 Credentials use the OS credential store or owner-private file storage, with descriptors only reading an already durable secret. Credentials never enter profile YAML, argv, logs, or redirects, HTTPS is required outside explicit development mode, and any new credential-store runtime module requires a contract D2 amendment.
- [ ] #5 Every built-in remote mutation has durable exact pending state before dispatch, including admin and no-handle requests. Named claims reuse handle pending state; renewal, completion, reconciliation, and other requests use bounded profile recovery records when that slot is occupied; a failed pre-dispatch write sends nothing.
- [ ] #6 Pending state retains each original request, authority, and `expectedRestoreId`. Guarded start and effect evidence remains until the operation is terminal or reconciled; a successful begin or renewal acknowledgment does not clear it, and recovering a later request cannot overwrite it. Each request record clears only after its confirmed terminal response, reconciliation, or definitive no-commit proof. Auth, revocation, incarnation, timeout, age, GC, replay expiry, refresh, and rotation preserve uncertainty.
- [ ] #7 A configured remote profile never falls back locally. An uncertain dispatch preserves evidence and stops dependent new work. Retained replay of a started operation never permits execution, and a late acknowledgment received after the safe dispatch window cannot cause first dispatch; a timely confirmed original start may dispatch its effect once.
- [ ] #8 Authority-time tests cover asymmetric latency, restart and suspend resampling, renewal at half TTL, stop-new-work at three quarters, the lower-bound 24-hour request deadline, expired short windows, and a late successful start response that does not dispatch an effect.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a narrow client-facing local/remote authority contract with a fake, leaving CLI and MCP consumer routing to TASK-107.9 and TASK-107.10.
2. Add trusted user-side profiles and project bindings with explicit/env/binding/default/local precedence, HTTPS enforcement, and owner-private credential descriptors/storage.
3. Implement strict remote HTTP envelopes, metadata discovery, enrollment, and exact durable request recovery for handleless and secondary mutations without redirect or local fallback.
4. Extend remote handles/pending evidence so guarded starts remain unresolved until terminal or reconciled, while later lifecycle recovery cannot overwrite the parent effect.
5. Add conservative authority-time sampling and half/three-quarter/deadline scheduling behavior.
6. Add focused contract tests, run repository quality gates, independently review and verify all acceptance criteria.
<!-- SECTION:PLAN:END -->
