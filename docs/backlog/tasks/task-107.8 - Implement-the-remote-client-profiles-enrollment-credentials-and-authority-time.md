---
id: TASK-107.8
title: >-
  Implement the remote client: profiles, enrollment, credentials, and authority
  time
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies:
  - TASK-107.7
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
The CLI and MCP adapter need one client library that carries the safety rules the design places on the client side: trusted profiles with an expected `authorityId` and pinned `restoreId`; user-side project bindings with the fixed precedence `--profile`, then `WORKLEASE_PROFILE`, then project binding, then user default, else local; no repository-committed configuration of any kind; credentials in the OS credential store or an owner-private file or descriptor; hidden, file, or descriptor invite input; no redirect following on credential-bearing requests; HTTPS required outside an explicit development mode; an immutable `expectedRestoreId` in every saved request envelope; the two-bound authority-time estimate (upper bound for expiry and renewal, lower bound plus 24 h for `requestNotAfter`) driving renewal at half the TTL and stop-new-work at three quarters; pending state saved before dispatch and cleared only on a confirmed terminal response; an optional best-effort receipt log; and never falling back to local state when a remote profile is configured.

Contract D2 restricts dependencies: OS credential store integration must either shell out to platform tools (macOS `security`, Linux `secret-tool`) or record a D2 amendment for a keyring module; the owner-private file and descriptor sources reuse the existing `--token-file`/`--token-fd` handling.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease profile add NAME --endpoint URL --authority ID` with hidden, `--invite-file`, or `--invite-fd` input reads the endpoint current `restoreId`, generates and durably stores an installation credential before dispatch, redeems the invite, pins `authorityId` and `restoreId`, and activates the profile only after confirmed or recovered enrollment; a dropped redemption response is recovered by exact replay without a second credential.
- [ ] #2 Profile selection follows the fixed precedence; `worklease profile bind NAME` writes a user-side binding for the canonical project identity; nothing in a repository checkout can select or redirect a profile; an unbound checkout with no flag, environment, or default uses the local authority and makes no network request.
- [ ] #3 Credentials live in the OS credential store or an owner-private file or descriptor, never in YAML, argv, or logs; credential-bearing requests do not follow redirects; plaintext HTTP is refused without an explicit development flag; changing an endpoint requires an explicit configuration action and a mismatched `authorityId` fails before any mutation.
- [ ] #4 Every saved remote request carries its origin `authorityId` and `expectedRestoreId`, which retry, refresh, rotation, and re-enrollment never rewrite; an `authority-restored` response fails closed, stops dispatch for that profile, and preserves pending requests as evidence.
- [ ] #5 Authority time is sampled from every response; renewal is scheduled at half the TTL and new guarded work stops at three quarters using the upper-bound estimate; `requestNotAfter` uses the lower-bound estimate plus 24 h; tests cover asymmetric latency, restart and suspend resampling, an expired short window, and a late start response that must not dispatch.
- [ ] #6 A configured remote profile never falls back to local state on any failure; a pre-dispatch pending-state write failure sends nothing; a failure after dispatch preserves unresolved intent and stops new work; pending state is never cleared by age, GC, or replay-window expiry.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
