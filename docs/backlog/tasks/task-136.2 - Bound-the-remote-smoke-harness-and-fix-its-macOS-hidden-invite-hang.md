---
id: TASK-136.2
title: Bound the remote smoke harness and fix its macOS hidden-invite hang
status: To Do
assignee: []
created_date: '2026-09-24 15:05'
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
- [ ] #2 When the hidden-invite child never prompts or never exits, `cliHiddenInvite` kills it and returns an error within a bounded time, covered by a unit test in `cmd/worklease-remote-smoke/main_test.go` that uses a fake child
- [ ] #3 No fixed sleep remains in the hidden-invite handoff
- [ ] #4 The harness writes per-group progress lines to stderr and, when its overall deadline fires, exits nonzero naming the step that was running
- [ ] #5 After a failed or timed-out run, no `worklease serve` process started by the harness is still running (checked with `pgrep -fl "worklease serve"`)
<!-- AC:END -->
