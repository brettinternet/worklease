---
id: TASK-136.1
title: Fix the known nondeterministic and environment-dependent test failures
status: Done
assignee:
  - '@brett'
created_date: '2026-09-24 15:05'
updated_date: '2026-09-24 18:12'
labels: []
dependencies: []
parent_task_id: TASK-136
priority: high
type: bug
ordinal: 52000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Three tests fail for reasons unrelated to the code under test. Each spurious failure teaches contributors to rerun CI instead of trusting it.

1. `internal/doctor` `TestDiagnoseClockAuthorityAllowsOneSecondSkew` failed on main in CI run 35819341946 (2026-09-23, linux-x64; the test took 2.33 s) with `doctor_test.go:202: over-one-second skew={ID:clock.authority Status:ok ...}`. The test writes `last_observed_at = time.Now()+2s`, and then `Diagnose` reads a fresh `time.Now()` (`internal/doctor/doctor.go` around lines 158–168). On a slow runner more than one second passes between the two reads, so the measured skew drops under the threshold.
2. `internal/lease` `TestRemoteBlankActorInstallationCanonicalizesMutationHashes` failed under `-race` on main in CI run 35658571052 (2026-09-21) with `remote_test.go:121: authority clock moved backward`, raised at `internal/lease/helpers.go:250`. `openRemoteLeaseTest` starts `testkit.NewClock` at wall time and never advances it. Something in the fixture, possibly `insertInstallation` or a store write, records real wall time as the authority watermark. When the race detector slows the test, that watermark passes the frozen fake clock.
3. `internal/queue` `TestBacklogScratchCLI` fails locally whenever `backlog` on PATH is a mise shim. Every package `TestMain` calls `testkit.IsolateProcessEnvironment`, which replaces HOME, and the shim then fails with `mise ERROR backlog is not a valid shim`. The test checks only `exec.LookPath`, so it fails instead of skipping. It passes in CI because mise-action puts real install directories on PATH.

Direction: make time an input the test controls. Do not widen tolerances or add retries. For doctor, evaluate the check against a clock the test supplies so the under- and over-threshold cases are exact. For lease, find the code that writes wall time and make the fixture agree with the fake clock. For the Backlog test, probe that `backlog --version` actually runs under the isolated environment and skip with the probe output if it does not. Keep the environment isolation as strict as it is today.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test -count=20 -run TestDiagnoseClockAuthorityAllowsOneSecondSkew ./internal/doctor` passes, and still passes with a temporary 3 s sleep between the fixture write and `Diagnose` (remove the sleep before committing)
- [x] #2 `go test -race -count=20 -run TestRemoteBlankActorInstallationCanonicalizesMutationHashes ./internal/lease` passes
- [x] #3 TestBacklogScratchCLI skips with a message naming the failed probe when `backlog` is a mise shim under the isolated HOME
- [x] #4 TestBacklogScratchCLI still runs to completion when a real Backlog.md binary is first on PATH (for example, prepend the bin directory reported by `mise where npm:backlog.md`)
- [x] #5 No tolerance was widened and no retry loop was added to make any of the three tests pass
- [x] #6 The queueindex cross-process lock test passes repeated runs after its observed failure cause is fixed, without retrying or weakening assertions.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inject a fixed observation time into doctor diagnostics while keeping the public entry point unchanged; test both skew boundaries and delayed diagnosis. 2. Initialize remote lease fixture clock at a fixed future instant and use WriteAt for fixture installation so wall-clock store writes cannot outrun the test clock. 3. Probe backlog --version before scratch CLI test; run focused repeated/race and both PATH variants, then repository gates and review.

4. Investigate the newly reproduced queueindex cross-process lock test flake (user-authorized scope expansion), fix its cause rather than retrying or widening tolerances, and rerun focused plus full gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Focused doctor count=20 and lease race count=20 pass. Doctor also passes after a temporary 3s delay (removed). Backlog scratch skips with failed probe output under a simulated broken mise shim and passes with the real installed CLI and node on PATH.

Commit hook uncovered a separate queueindex cross-process lock flake (2 failures in 20 focused runs); user authorized adding it to this task. Staged code remains uncommitted until fixed and verified.

Diagnosed queueindex flake: 8 helpers concurrently initialize the brand-new SQLite index, and some fail Open with SQLITE_BUSY before testing the lock (captured child stdout). Initialize the disposable index once in the lock test fixture, then exercise the same 8-process lock contention; count=20 and race count=3 pass. Simultaneous first-time index Open remains a separate product concern; no production behavior changed.

Review: one diff review, no remaining item-scoped defect. Validation after queueindex fixture correction: mise run lint, format-check, test, typecheck, hooks passed; doctor count=20 and delayed diagnosis, remote lease race count=20, scratch CLI simulated broken shim SKIP and real binary PASS, queueindex count=20 and race count=3. Code commit fc0072e fast-forward merged to main; worktree and branch removed, associated Herdr workspace closed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Stabilized doctor and remote-lease clocks, skipped unusable Backlog CLI shims, and isolated the queueindex lock test from concurrent SQLite schema initialization. Verified focused repeated/race tests and all repository gates; merged fc0072e to main and cleaned the worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
