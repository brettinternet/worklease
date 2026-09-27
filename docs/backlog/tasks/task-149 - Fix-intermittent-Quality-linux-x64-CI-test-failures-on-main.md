---
id: TASK-149
title: Fix intermittent Quality (linux-x64) CI test failures on main
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-26 16:49'
updated_date: '2026-09-27 05:05'
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

5. Diagnose post-publication linux CI failures (credential helper startup and replaced-checkout cache lineage), apply focused fixes or file proven root causes; validate and reevaluate criterion #2.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CI diagnosis: on 2-vCPU Quality runner, mise ci scheduled test, race, lint, vet, vuln, e2e and man concurrently (confirmed by mise run --dry-run ci). Historical failures include exec errno 26 in adapter startup (run 36231177947), credential-helper startup failures (36254185266), and CLI isolated-test deadline exhaustion on macOS x64 (36225574287). Changed mise ci to run gates serially without retrying tests or increasing timeouts. Focused GOMAXPROCS=1 go test -race -count=50 passed the four named queue tests together and the two CLI Start work tests together. Full mise run ci passed (format, staticcheck, vet, test, race, vuln, e2e, man); one direct review pass found no additional scoped defect. Historical CI observations are not ten post-change main CI runs; #2 requires live evidence after delivery.

Criterion #1 checked after GOMAXPROCS=1 go test -race -count=50 on the four named internal/queue tests and separately on the two named internal/cli tests; both commands returned exit 0.

Merged tested configuration commit 7fd6f3e into local main (fast-forward); cleaned session-created worktree and branch. Main is ahead of origin and was not pushed (no push authorization). Remaining criterion #2: after authorized publication, inspect ten consecutive Quality (linux-x64) runs for this change; if any fail, file the specific failure and proven root cause before marking Done. Latest remote main run 36256839244 succeeded but predates 7fd6f3e. No post-change remote CI result exists.

Post-publication main CI 36280444961 failed Quality (linux-x64): TestGitHubProjectStatusWritesUseRecoveryAndNeverRedispatchLostResponse/lost-response-true reported credential helper failed; TestNewBacklogCommitShowsPreviousRowsStale returned predecessor cache rows (also linux-arm64). Commit 7fd6f3e is on origin/main. Investigating both; preserve unrelated primary checkout changes.

Follow-up commits 0dd098e (replaced checkout lineage includes creation time; GitHub test helper re-executes the stable test binary instead of a writable shell script, with safe failure details) and 1b76374 (split race suite by TestQueue prefix to avoid CLI Go test deadline). Focused race tests for GitHub recovery, credential helper diagnostic, and replaced checkout passed 3 repetitions; lint, vet, format-check, and queue/queueindex tests passed. New main run 36294294394 also exposed TestQueueNextStartOutcomes isolated child reaching Go per-package deadline under race; split follows the existing compatibility job boundary. Await GitHub main CI validation before checking criterion #2.
<!-- SECTION:NOTES:END -->
