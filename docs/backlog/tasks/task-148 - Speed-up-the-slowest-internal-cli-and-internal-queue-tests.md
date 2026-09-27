---
id: TASK-148
title: Speed up the slowest internal/cli and internal/queue tests
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-26 16:39'
updated_date: '2026-09-27 00:38'
labels:
  - testing
dependencies: []
priority: medium
ordinal: 82000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The pre-commit hook tests packages with staged Go files, so any edit to internal/cli (~244s) or internal/queue (~115s) still makes a commit wait minutes. A timing pass (go test -count=1 -json -p 1 -parallel 2) found the time is concentrated in a small number of tests: 32 of 337 cli tests account for ~307s of reported elapsed time, and 15 of 281 queue tests for ~110s. Elapsed includes time parallel tests spend paused, so treat the numbers as relative.

Slowest: TestQueueNextStartOutcomes 62.6s, TestBeadsQueueNextClaimAndMCP 62.0s, TestQueueWriteControllerPreviewsAndVerifiesBacklogMutation 28.7s, TestBeadsWriteReadbackKeepsGitStage 26.4s, TestQueueStartWorkComposesClaimAndProviderTransition 23.5s, TestQueueNextStartAppliesBacklogTransition 17.6s, TestMCPQueueNextStartAppliesBacklogTransition 17.5s, TestBeadsAdapterConformance 15.0s, TestBeadsWritePipelineCheckpointAndRecovery 13.9s, TestQueueStartWorkRevalidatesPrerequisitesBeforeClaim 10.1s.

These tests run the real bd and backlog CLIs plus Git many times per case (each backlog/bd call costs ~0.2s even warm), and several re-run the same scenario in both CLI and MCP modes. Options: build the provider fixture once per test and copy it; cover the CLI/MCP split once at the lowest layer instead of repeating slow provider round-trips (see AGENTS.md Testing rules); fake the provider where the provider is not the behavior under test; move real-provider round-trips to e2e.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Each listed test either runs in under 5s or its real-provider round-trip is covered once and justified in the test
- [ ] #2 go test -count=1 ./internal/cli completes in under 90s and ./internal/queue in under 45s with the mise task flags
- [ ] #3 No behavior loses coverage; any moved coverage is named in the task notes
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Profile listed cases and inspect existing test fixtures/callers to remove repeated provider process setup without losing coverage. 2. Reduce duplicate CLI/MCP round-trips at the lowest suitable layer and document any retained integration checks. 3. Measure both packages with the mise test flags, run focused race tests and repository checks; review once, integrate and record evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Initial suite measured with mise exec -- go test -count=1 -p 1 -parallel 2: CLI 229.5s, queue 102.8s. Reduced six real Backlog queue-next outcome scenarios to three (one per outcome; both transports still have success tests); isolated the 62s Beads CLI test so it can run in parallel; moved the redundant Beads pipeline/recovery assertions to existing generic pipeline tests, retaining provider comment-count evidence in the Beads write/readback test. Current suite passes but remains above targets (CLI 189.1s, queue 109.7s). Next: reduce serial real-provider CLI setup and queue provider round trips without losing coverage; rerun focused race and measure.

Focused race checks passed: go test -race -count=3 on TestQueueNextStartOutcomes and TestBeadsQueueNextClaimAndMCP (internal/cli), and TestBeadsWriteReadbackKeepsGitStage plus TestWritePipelineRecoveryNeverRedispatches (internal/queue). Hook passed gofmt and affected-package tests (CLI 179.9s, queue 97.5s). Direct diff review found no further item-scoped defect. Commit aa7a3f9 contains the test changes; targets remain unmet, so task stays In Progress. Note: an unadorned go test in this worktree can fail where testkit.Home hides mise shims; use mise exec -- go test.

Merged aa7a3f9 into local main in 207a1d0 after committing task progress as abd4b52. Post-merge mise run lint, format-check, typecheck, test all passed; internal/cli 177.4s and internal/queue 95.9s, still above 90s/45s acceptance. Worktree task-148-speed-tests and branch removed with Worktrunk; Herdr workspace hook closed exact linked workspace. No push. Next owner should investigate moving unique provider round-trips to a CI-run integration tier or replacing non-provider scenarios with bounded fakes; preserve adapter-level real round-trip and measure with -p 1 -parallel 2. Do not mark Done until all three criteria are objectively met.
<!-- SECTION:NOTES:END -->
