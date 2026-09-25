---
id: TASK-143.1
title: >-
  Add `worklease queue adapter check` to run conformance against any adapter
  executable
status: Done
assignee: []
created_date: '2026-09-25 16:38'
updated_date: '2026-09-25 18:21'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies: []
documentation:
  - docs/external-adapter-protocol.md
parent_task_id: TASK-143
priority: medium
type: feature
ordinal: 73000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The host conformance suite (TASK-133.3) proves adapters through the production host, but it runs only as `go test ./internal/queue` against fixture executables built in this repository. An author cannot run it against their own binary, so the protocol's guarantees are untestable outside the repo, and adapters get approved on the evidence of a process merely starting.

The user wants the CLI approached with API-driven design: every command added here is a stable, scriptable interface first, with `--json` output in the existing CLI envelope (`schemaVersion`, `ok`, `operation`, and `error.reason`/`exitCode`/`details`), stable diagnostic reasons, and documented exit codes, and the human output is a rendering of that same result.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `worklease queue adapter check --executable PATH` runs the conformance checks through the production host against an arbitrary executable, with adapter config from a flag or file, without writing queue.yaml, approval state, the queue index, or any Worklease authority.
- [x] #2 Checks cover initialize and version negotiation, manifest validation, required methods, pagination and continuation, deadlines and budgets, `$/cancelRequest`, frame and collection limits, malformed and oversized output, crash handling, stderr and output secret redaction, and `resourcePolicy` inputs.
- [x] #3 When the manifest declares mutation, checks also cover dispatch receipts, lost-response recovery through `readReceipt` without redispatch, and `unknown` outcomes, against a caller-supplied disposable target; mutation checks never run without an explicit flag naming that target.
- [x] #4 `--json` emits a versioned result with one entry per check (ID, status pass/fail/skip, stable reason, bounded redacted detail) and an overall verdict; the exit code distinguishes pass, conformance failure, and usage or environment error, and all three are documented.
- [x] #5 Skipped checks state why (for example an undeclared capability) and never count as passes.
- [x] #6 The in-repo conformance tests and the new command share one check implementation, so the built-in fixtures and the sample adapter pass through the command in tests.
- [x] #7 The CLI reference and authoring guide document the command, its JSON schema, and exit codes.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extract reusable production-host conformance checks from the existing fixture suite and expose a side-effect-free check runner for arbitrary executables/config. 2. Add stable JSON/human CLI results and explicit guarded mutation target; test built-in fixtures and sample through command. 3. Document command and run focused/full gates, review, commit, integrate and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented external adapter check through the production supervised host without queue/approval/authority writes. Shared checker exercised by built-in Backlog.md, GitHub, and sample fixture tests; CLI Run exercised with sample and failure/usage/redaction cases. Optional cancellation marker fixture observes $/cancelRequest; disposable target gates receipt/readback/unknown probes. Reviewer found six concrete defects (relative path, unauthorized progress, long pagination, independent claim source, cancellation, usage operation), corrected and covered with focused tests. lint, format-check, test, typecheck and focused race count=3 passed.

Objective acceptance: internal/cli TestQueueAdapterCheckSampleWithoutApprovalOrQueueState verifies sample CLI, inline/file config, no queue/approval writes, JSON pass/fail/usage exits; TestQueueAdapterCheckDoesNotEchoAdapterSecrets covers malicious stdout/stderr. internal/queue TestAdapterConformance runs Backlog.md, GitHub and sample fixtures through both shared engine and public CLI; TestAdapterConformanceMutationRequiresExplicitDisposableTarget checks no write without target, receipt/readback/unknown with exactly one dispatch; TestAdapterConformanceDetectsIgnoredCancellation and built-in cancel-marker fixture check notifications; TestAdapterConformanceSkipsUnauthorizedProgress and LargeSourceAndIndependentClaimSource cover authorization and bounded pagination. docs/cli-reference.md and external-adapter-authoring.md specify JSON and exit codes. Review findings corrected; no second general review. Commits: 12c6c94, merge f7e5171, test isolation 9e467b2, CLI fixtures a24a162. Final main gates lint, format-check, test, typecheck, hooks; changed tests race count=3 passed. Remaining blocker: none. Next step: release claim; dependent TASK-143.3 may start.

Delivery confirmed on main: code merge f7e5171, test stability 9e467b2, CLI fixture integration a24a162, task completion 5f81d78. Worktrunk worktree and branch task-143-1-adapter-check removed after confirming merged ancestry; Herdr workspace wQW closed after inspecting its sole shell pane. Claim released (assignee cleared). No remaining blocker or further action for TASK-143.1; next eligible dependent is TASK-143.3.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shipped the scriptable external-adapter conformance CLI with isolated host checks, guarded disposable writes, bounded redacted diagnostics, and documented JSON/exit codes. Backlog.md, GitHub, sample and mutation fixtures passed CLI/engine checks; race count=3, full lint/format/test/typecheck and hooks passed. Merged to main in f7e5171; follow-up test commits 9e467b2 and a24a162.
<!-- SECTION:FINAL_SUMMARY:END -->
