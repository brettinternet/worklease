---
id: TASK-154
title: Reduce subprocess-heavy race test runtime without losing coverage
status: Done
assignee: []
created_date: '2026-10-21 01:56'
updated_date: '2026-10-22 07:09'
labels:
  - testing
  - ci
  - reviewed
dependencies:
  - TASK-156
references:
  - 'https://github.com/brettinternet/worklease/actions/runs/39329277019'
  - internal/testkit
  - internal/cli
  - internal/queue
  - docs/backlog/tasks/task-148
priority: high
type: enhancement
ordinal: 98000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The v1.9.3 release exposed a longstanding CI bottleneck. Successful CI run 39329277019 spent 1178.28s in race tests out of a roughly 24-minute Linux quality job; normal tests took 122.38s. Race package timings were 487.449s for CLI TestQueue cases, 285.569s for internal/queue, and 118.240s for other CLI cases. TASK-148 previously reduced real-provider duplication but did not resolve this race overhead. Re-executed test binaries and the race runtime exit delay are hypotheses to measure, not established root causes. Preserve provider safety checks, process cleanup, and race detection; do not obtain speed by skipping coverage or widening timeouts.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Record repeatable per-test and package race timings with provider tests enabled, distinguishing test execution, subprocess overhead, and build time; identify measured dominant causes.
- [x] #2 Reduce the measured dominant test overhead and record before/after timings on equivalent runner resources, including total CPU or runner time rather than only sharded wall time.
- [x] #3 All existing behavioral and provider coverage remains exercised; changed process helpers retain bounded cleanup and surface race failures, verified by focused repeated race tests and a successful full CI run.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Preserve the measured baseline and per-test/package timings; isolate the two profile-binding tests from the implementation checkout so full checks work in linked worktrees.
2. Set only atexit_sleep_ms=0 in the mise race task, preserving other caller GORACE options. Add a race-only subprocess regression proving races still produce diagnostics and nonzero exit; leave timeout/process-group cleanup unchanged.
3. Measure full before/after on this same machine with repeated focused checks, run all quality gates, independently review the race-failure boundary once, and record evidence. Obtain full exact-commit CI if push is authorized; commit/merge locally and clean up as requested.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
2026-10-21: The measured dominant costs (CLI TestQueue cases at 487 s and internal/queue at 286 s of race time) disappear with TASK-156. Re-measure after it lands, then target what remains. If the race job is no longer a material bottleneck, record the measurement and close this task with that evidence.

Baseline post-queue measurements on this macOS arm64 machine, Go 1.27.3, providers available and no skips: warm race run 125.266s wall / 36.013s user / 12.688s system; normal 55.284s / 21.091s / 11.394s; race compile/link-only (go test -exec /usr/bin/true) 3.445s. GORACE=atexit_sleep_ms=0 diagnostic full run 73.927s / 35.529s / 12.367s: ~51s exit-wait overhead rather than build or CPU. Both baseline and diagnostic expose the same two linked-worktree profile-test failures (fixtures read checkout-root bindings while production writes common-root bindings). Logs and timing script: /tmp/worklease-task154-*. No tests skipped.

Equivalent same-tree comparison (including fixture isolation and new race-exit regression), same macOS arm64 host and Go1.27.3, -p1/-parallel2, no sharding, warm cache, -count=1: default exit delay 128.332s wall,36.804s user,13.966s system; zero delay run1 74.476s/35.838s/12.922s; run2 72.515s/35.720s/12.779s. Wall/runner duration falls 42.0–43.5%; total CPU is essentially unchanged (50.770s ->48.760/48.500s). Full pass identities identical:913 test/subtest events across25 tested packages; zero skipped tests (four package skip events are packages with no test files). No provider suites remain after TASK-156; all remaining tests ran, plus built-binary and remote smoke/doc examples.
Repeat: go test -race -p 1 -parallel 2 -count=1 -json ./... (default), then GORACE=atexit_sleep_ms=0 with the identical command. Compile/link-only warm control: go test -race -p 1 -exec /usr/bin/true -json ./... (3.445s wall). Python subprocess runner measured time.monotonic and resource.getrusage(RUSAGE_CHILDREN) deltas, including builds and children. Full JSON/timing artifacts /tmp/worklease-task154-{corrected-baseline,after-1,after-2}.*.
Package seconds before -> after2: CLI47.594->31.519; guard10.277->6.186; lease10.800->3.685; MCP9.923->9.104; store7.119->3.115; testkit10.494->6.403. Every other tested package saves approximately its1s process-exit delay. Representative test seconds: TestOverlappingBundlesAcrossProcessesHaveOneWinner5.59->0.49; TestRunIsolatedTestPreservesSelectionAndSkip4.10->0.08; TestExecLiteralArgvBoundedInvalidUTF8SignalAndEnvironment3.37->0.30; TestRunTestProcessSuccessAndBoundedCleanup4.10->2.08 (intentional2s cleanup timeout unchanged); TestProcessesSerializeOneHandleAndConcurrentMutations2.45->0.47. Warm isolated helper probe -race -count=3 was14.084s before; its bodies were near-zero, exit sleeps dominated.
Validation: full lint, format-check (fixed gofmt alignment then reran successfully), typecheck, normal tests, race task, e2e (local/remote shipped binaries and doc examples), vulnerability scan, staged hooks all passed. Focused changed profile tests and all process-helper tests passed -race -count=3. New race-only regression verifies deliberate unordered child writes still emit WARNING: DATA RACE and preserve configured nonzero exitcode77 even after os.Exit(0). Production process helper and its process-group cleanup/timeouts unchanged. Direct one-pass risk review found no defects; optional independent reviewer launch was rejected by configured model scope, so no reviewer result is claimed. User explicitly authorized push main after local merge to obtain full CI; criterion3 remains pending until that succeeds.

Implementation committed as c856d73 (Remove race-test process exit delays) in session-owned worktree, fast-forward merged to main. Full CI pending on push; then record run evidence and mark Done. Owned checkout receipt: .git/worktrees/task-154/agent-creation.json, session01a161f2-0ba8-7288-ae92-fa592b87c826, loopc1e7ab2c-e80b-4cb7-b494-91c34b6a286f; checkout /Users/brett/dev/me/worklease/.worktrees/task-154; branch task-154; creationb4c93f968a47189970d91f1117eb957d8482553f.

Full CI passed: https://github.com/brettinternet/worklease/actions/runs/39726608192, exact commit f9b0f08415eeebeeda2d1ed9ece616e37fcc8f25 (includes implementation c856d73362c281a0c2229ad8a6263061d86773c1). Quality linux-x64 and compatibility linux-arm64, macos-x64, macos-arm64 all succeeded. Linux quality job completed in 6m23s including full quality gates, race tests, shipped-binary/remote/documentation smoke and generated-manual validation; this is not an equivalent-runner before/after comparison to pre-queue CI. All acceptance criteria satisfied. Session-owned task-154 checkout and branch removed together by wt remove --foreground; only primary checkout remains. Herdr workspace listing confirms no workspace for the removed checkout; workbench plugin was unavailable, so verified via Herdr CLI. No retained implementation checkout/workspace or remaining blocker.

Review 2026-10-22: no issues. Race regression and isolated profile tests pass go test -race -count=3; race partition lists cover all 29 packages (1 cli + 28 rest).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed race-runtime exit sleeps from the race task while retaining caller GORACE options, instrumentation, and failure exits. Added a deliberate child-race regression and isolated two profile fixtures from linked-worktree binding differences. Equivalent local full-suite measurements improved from128.3s to72.5–74.5s (42–43.5% less runner time), with unchanged913 passing test/subtest identities and essentially unchanged CPU usage. Focused repeated race checks, full local gates, and all four platform jobs in CI39726608192 passed. Committed as c856d73, merged and pushed to main; owned worktree/branch cleaned up.
<!-- SECTION:FINAL_SUMMARY:END -->
