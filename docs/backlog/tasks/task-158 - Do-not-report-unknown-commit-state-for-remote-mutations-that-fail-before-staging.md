---
id: TASK-158
title: >-
  Do not report unknown commit state for remote mutations that fail before
  staging
status: To Do
assignee: []
created_date: '2026-10-23 22:39'
labels: []
dependencies: []
ordinal: 101000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
When a remote acquire times out before the pending request is staged (for example while `RemoteAuthority.dispatch` samples clock/metadata from a stalled authority, internal/authority/authority.go ~177), `mutationFailure` (internal/cli/lease_commands.go ~722) returns remote-transport-failure with `commitState: unknown` and a `pendingPath` that was never written. `handle inspect --session` then reports the handle missing, so callers following the documented recovery (retain session, replay exact handle) have nothing to replay and cannot tell whether anything was sent. Repro: SIGSTOP the `worklease serve` process, run `worklease --json acquire --resource coordination:x --wait 0s --session s`, then `handle inspect --session s`. Found while verifying bl remote claims (backlog BL-72).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A remote mutation that fails before its request is staged reports `commitState: not-committed` (or an equivalent definite state) and no `pendingPath`
- [ ] #2 Any `pendingPath` in a failure result refers to a staged request that `handle inspect` and `acquire --handle` can read
- [ ] #3 Tests cover a pre-staging metadata/clock timeout and a post-staging transport failure
<!-- AC:END -->
