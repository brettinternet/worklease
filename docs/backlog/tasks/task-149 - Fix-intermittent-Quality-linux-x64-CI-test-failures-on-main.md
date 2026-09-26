---
id: TASK-149
title: Fix intermittent Quality (linux-x64) CI test failures on main
status: To Do
assignee: []
created_date: '2026-09-26 16:49'
labels:
  - testing
  - ci
dependencies: []
priority: high
ordinal: 83000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Quality (linux-x64) job on main has failed on 8 of the last 13 completed CI runs, and a different test fails almost every time. Platform matrix jobs mostly pass. Failures seen:
- TestGitHubProjectWritesRequireBothConfigurationGates/read-only_token, 'authentication: credential helper failed' (runs 36254185266, 36210119186)
- TestExternalAdapterRejectsUndeclaredCredentialFeature (36210119186)
- TestAdapterConformanceDetectsIgnoredCancellation (36231177947)
- TestAdapterConformanceRejectsEarlyCancellationDoneMarker (36227278300)
- TestQueueStartWorkComposesClaimAndProviderTransition and TestQueueStartWorkRevalidatesMappingBeforeClaim (36225574287)

These all involve subprocess credential helpers, adapter processes, or cancellation timing, so they likely race under load on the 2-vCPU runner. The linux-x64 job used to run the full suite twice (once in the hook step, once in mise run ci); 2abd13d reduced that to once. Reproduce with go test -race -count=20 -run <name> ./internal/queue under CPU load (for example stress or GOMAXPROCS=1), fix each root cause, and don't retry or widen tolerances (AGENTS.md Testing rules).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Each listed test passes go test -race -count=50 -run <name> with GOMAXPROCS=1
- [ ] #2 Ten consecutive main CI runs pass Quality (linux-x64), or any remaining failure is filed with its root cause
<!-- AC:END -->
