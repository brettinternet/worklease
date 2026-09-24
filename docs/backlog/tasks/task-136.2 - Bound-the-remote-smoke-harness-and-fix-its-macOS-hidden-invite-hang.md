---
id: TASK-136.2
title: Bound the remote smoke harness and fix its macOS hidden-invite hang
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-24 15:05'
updated_date: '2026-09-24 19:47'
labels: []
dependencies: []
parent_task_id: TASK-136
priority: high
type: bug
ordinal: 53000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`go run ./cmd/worklease-remote-smoke --binary ./bin/worklease` runs as part of `mise run e2e` and `mise run ci`. On macOS arm64 it hangs forever: two of two local runs on 2026-09-24 printed nothing and were killed after 10 minutes and after 150 s. A SIGQUIT goroutine dump showed `main` blocked in `cmd.Wait()` inside `(*harness).cliHiddenInvite` (`cmd/worklease-remote-smoke/main.go`, around line 2403), called from `group3EnrollmentFaults` (around line 2933) for the hidden-prompt `enroll`. CI runs the harness only on linux-x64, where it passes in about 40 s. The macOS matrix jobs run only `go test ./...`, so CI never sees the hang.

Defects visible in `cliHiddenInvite`:
- If the `Invite: ` prompt does not appear within 5 s, it records an error but still calls `cmd.Wait()` with no deadline, while the child waits on the PTY forever.
- After the prompt appears, it sleeps 50 ms and assumes the child has turned off echo by then (`term.ReadPassword` in `internal/cli/profile_commands.go`, around lines 335–338) before it writes the invite. That is a timing guess.

The harness as a whole prints nothing until it finishes and has no overall deadline, so any stuck step looks like a silent hang. One interrupted run also left a `worklease serve --server-config ...` child process running.

Direction: give every child process the harness starts a timeout that kills it. Make the PTY handoff wait for something it can observe, such as the PTY ECHO flag turning off, instead of sleeping. Print group and step progress to stderr, and exit nonzero naming the running step when an overall deadline fires. Find out why the handoff stalls on macOS and fix that cause. Do not skip the hidden-invite scenario on darwin.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The remote smoke passes locally on macOS arm64 and still passes in CI on linux-x64
- [x] #2 When the hidden-invite child never prompts or never exits, `cliHiddenInvite` kills it and returns an error within a bounded time, covered by a unit test in `cmd/worklease-remote-smoke/main_test.go` that uses a fake child
- [x] #3 No fixed sleep remains in the hidden-invite handoff
- [x] #4 The harness writes per-group progress lines to stderr and, when its overall deadline fires, exits nonzero naming the step that was running
- [x] #5 After a failed or timed-out run, no `worklease serve` process started by the harness is still running (checked with `pgrep -fl "worklease serve"`)
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce hidden-invite stall and inspect process/PTY lifecycle. 2. Bound child processes and harness deadline with cleanup and progress diagnostics. 3. Replace prompt timing guess with observable terminal state; test no-prompt/no-exit failures. 4. Run focused race tests, local macOS e2e, project gates, then review and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Reproduced macOS hang: PTY emitted terminal color/cursor probes (OSC 11 and CSI 6n) that the harness cannot answer, so the invite prompt never appeared. TERM=dumb/NO_COLOR suppress probes while retaining interactive stdin. Hidden invite and harness deadlines, process cleanup, and progress were implemented; macOS local smoke passed all five groups once, pgrep showed no serve child.

Review found three concrete timeout risks: descendant-held output pipes, remote server surviving failed replacement SSH cleanup, and independent child contexts. Fixed with process-group cancellation/WaitDelay, remote serve stdin-EOF lifecycle plus bounded reaping, and inherited deadline; focused race tests (3 runs), full macOS arm64 e2e, lint, format-check, test, typecheck, and Linux/amd64 test-binary compile pass. Native linux-x64 CI remains unverified without an authorized pushed commit/runner.

Merged fe51ed9217a277494f4a02cc719589a89ffb451e into local main by fast-forward; removed owned worktree/branch through Worktrunk. AC #2: fake PTY children without prompt or exit are killed and reaped in race-count=3. AC #3: hidden prompt now waits for terminal ECHO-off (no fixed sleep); full macOS smoke exercised it. AC #4: deadline fake child reports the active step; group and step progress appeared in e2e stderr. AC #5: pgrep -fl "worklease serve" returned no processes after both a failed and a passing real smoke; timed-out fake server cleanup and remote-helper SSH-EOF are covered by focused tests.
<!-- SECTION:NOTES:END -->
