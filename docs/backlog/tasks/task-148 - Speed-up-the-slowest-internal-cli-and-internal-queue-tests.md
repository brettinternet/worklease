---
id: TASK-148
title: Speed up the slowest internal/cli and internal/queue tests
status: To Do
assignee: []
created_date: '2026-09-26 16:39'
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
