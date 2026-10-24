---
id: TASK-158
title: Do not report unknown commit state for remote mutations that fail before staging
status: done
labels: [reviewed]
rank: ab73dfa9c4b73df08
created: 2026-10-23T22:39:00Z
updated: 2026-10-24T01:30:10Z
---

When a remote acquire times out before the pending request is staged (for example while `RemoteAuthority.dispatch` samples clock/metadata from a stalled authority, internal/authority/authority.go ~177), `mutationFailure` (internal/cli/lease_commands.go ~722) returns remote-transport-failure with `commitState: unknown` and a `pendingPath` that was never written. `handle inspect --session` then reports the handle missing, so callers following the documented recovery (retain session, replay exact handle) have nothing to replay and cannot tell whether anything was sent. Repro: SIGSTOP the `worklease serve` process, run `worklease --json acquire --resource coordination:x --wait 0s --session s`, then `handle inspect --session s`. Found while verifying bl remote claims (backlog BL-72).

## Acceptance criteria

- [x] A remote mutation that fails before its request is staged reports `commitState: not-committed` (or an equivalent definite state) and no `pendingPath`
  Evidence: go test -race -count=3 -run TestRemoteAcquireFailureRecoveryMatchesStaging ./internal/cli passed: metadata timeout reports not-committed, no pendingPath or recoveryHint, and no handle file. Transfer metadata timeout also passes.
- [x] Any `pendingPath` in a failure result refers to a staged request that `handle inspect` and `acquire --handle` can read
  Evidence: go test -race -count=3 -run TestRemoteAcquireFailureRecoveryMatchesStaging ./internal/cli passed: lost post-staging acquire response reports unknown with a readable pendingPath; handle inspect reports pendingRequestPresent and acquire --handle replays successfully to a ready handle.
- [x] Tests cover a pre-staging metadata/clock timeout and a post-staging transport failure
  Evidence: go test -race -count=3 -run 'TestTransferMetadataTimeoutIsNotCommitted|TestRemoteAcquireFailureRecoveryMatchesStaging' ./internal/authority ./internal/cli passed (three repetitions): metadata deadline and post-commit lost transport response both covered.

## Notes

Plan: classify failed remote metadata sampling as not-committed before dispatch; emit pendingPath only when a readable staged handle exists; preserve this classification for transfer. Add CLI regressions for metadata timeout without a handle and lost post-staging response with inspect/exact replay. Run focused repeated race checks, required gates, one scoped review, commit and merge to main, then approved cleanup. Worktree creation: task-158 at /Users/brett/dev/me/worklease/.worktrees/task-158, base 7330b3e; receipt hook failed on empty stdin; explicit user approval authorizes cleanup of this checkout and branch after merge.

Implemented and fast-forward merged d8b3a42 into main. Metadata sampling failures now report not-committed; CLI pendingPath requires a readable staged request; transfer preserves dispatch failure classification. One scoped review completed; fixed transfer masking of definite pre-dispatch errors and added regression coverage. LSP clean; focused race tests passed three repetitions; mise run lint, format-check, typecheck, test, and hooks all passed. All acceptance criteria verified; no remaining blocker. Next: mark done, commit authoritative task record, and perform explicitly approved worktree/branch cleanup (creation receipt hook failed; user approved this exact checkout).

Delivery complete: d8b3a42 merged into main. Approved wt remove task-158 --foreground --format=json deleted the worktree and branch; Git lists only the primary checkout and Herdr workspace wNS is confirmed absent. No retained task checkout/workspace, no remaining blocker or implementation step. Completion record will be committed on main.

Review: a metadata-sampling failure was marked not-committed even when an earlier attempt of the same operation was already staged (handle or pending store), which could mislabel a possibly committed operation. dispatch now leaves commitState unclassified (CLI reports unknown) when the operation is staged. Extended TestTransferMetadataTimeoutIsNotCommitted; confirmed it fails without the fix. Full gates passed.
