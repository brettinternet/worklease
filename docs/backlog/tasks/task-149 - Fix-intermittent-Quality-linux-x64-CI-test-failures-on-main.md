---
id: TASK-149
title: Fix intermittent Quality (linux-x64) CI test failures on main
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-26 16:49'
updated_date: '2026-09-26 23:17'
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
- [x] #1 Each listed test passes go test -race -count=50 -run <name> with GOMAXPROCS=1
- [ ] #2 Ten consecutive main CI runs pass Quality (linux-x64), or any remaining failure is filed with its root cause
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect historical failures and constrained race runs to isolate resource contention. 2. Serialize quality gates to prevent overlapping Go suites and process-heavy checks on small CI runners. 3. Verify each named test for 50 race repetitions, run all quality gates, commit and merge. 4. Monitor post-merge main CI; record any remaining failure with a concrete root cause before closing.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CI diagnosis: on 2-vCPU Quality runner, mise ci scheduled test, race, lint, vet, vuln, e2e and man concurrently (confirmed by mise run --dry-run ci). Historical failures include exec errno 26 in adapter startup (run 36231177947), credential-helper startup failures (36254185266), and CLI isolated-test deadline exhaustion on macOS x64 (36225574287). Changed mise ci to run gates serially without retrying tests or increasing timeouts. Focused GOMAXPROCS=1 go test -race -count=50 passed the four named queue tests together and the two CLI Start work tests together. Full mise run ci passed (format, staticcheck, vet, test, race, vuln, e2e, man); one direct review pass found no additional scoped defect. Historical CI observations are not ten post-change main CI runs; #2 requires live evidence after delivery.

Criterion #1 checked after GOMAXPROCS=1 go test -race -count=50 on the four named internal/queue tests and separately on the two named internal/cli tests; both commands returned exit 0.

Merged tested configuration commit 7fd6f3e into local main (fast-forward); cleaned session-created worktree and branch. Main is ahead of origin and was not pushed (no push authorization). Remaining criterion #2: after authorized publication, inspect ten consecutive Quality (linux-x64) runs for this change; if any fail, file the specific failure and proven root cause before marking Done. Latest remote main run 36256839244 succeeded but predates 7fd6f3e. No post-change remote CI result exists.
<!-- SECTION:NOTES:END -->
