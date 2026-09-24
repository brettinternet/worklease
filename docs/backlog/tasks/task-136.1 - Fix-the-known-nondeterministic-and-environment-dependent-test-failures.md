---
id: TASK-136.1
title: Fix the known nondeterministic and environment-dependent test failures
status: To Do
assignee: []
created_date: '2026-09-24 15:05'
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
- [ ] #1 `go test -count=20 -run TestDiagnoseClockAuthorityAllowsOneSecondSkew ./internal/doctor` passes, and still passes with a temporary 3 s sleep between the fixture write and `Diagnose` (remove the sleep before committing)
- [ ] #2 `go test -race -count=20 -run TestRemoteBlankActorInstallationCanonicalizesMutationHashes ./internal/lease` passes
- [ ] #3 TestBacklogScratchCLI skips with a message naming the failed probe when `backlog` is a mise shim under the isolated HOME
- [ ] #4 TestBacklogScratchCLI still runs to completion when a real Backlog.md binary is first on PATH (for example, prepend the bin directory reported by `mise where npm:backlog.md`)
- [ ] #5 No tolerance was widened and no retry loop was added to make any of the three tests pass
<!-- AC:END -->
