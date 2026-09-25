---
id: TASK-143.1
title: >-
  Add `worklease queue adapter check` to run conformance against any adapter
  executable
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-25 16:38'
updated_date: '2026-09-25 17:01'
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
- [ ] #1 `worklease queue adapter check --executable PATH` runs the conformance checks through the production host against an arbitrary executable, with adapter config from a flag or file, without writing queue.yaml, approval state, the queue index, or any Worklease authority.
- [ ] #2 Checks cover initialize and version negotiation, manifest validation, required methods, pagination and continuation, deadlines and budgets, `$/cancelRequest`, frame and collection limits, malformed and oversized output, crash handling, stderr and output secret redaction, and `resourcePolicy` inputs.
- [ ] #3 When the manifest declares mutation, checks also cover dispatch receipts, lost-response recovery through `readReceipt` without redispatch, and `unknown` outcomes, against a caller-supplied disposable target; mutation checks never run without an explicit flag naming that target.
- [ ] #4 `--json` emits a versioned result with one entry per check (ID, status pass/fail/skip, stable reason, bounded redacted detail) and an overall verdict; the exit code distinguishes pass, conformance failure, and usage or environment error, and all three are documented.
- [ ] #5 Skipped checks state why (for example an undeclared capability) and never count as passes.
- [ ] #6 The in-repo conformance tests and the new command share one check implementation, so the built-in fixtures and the sample adapter pass through the command in tests.
- [ ] #7 The CLI reference and authoring guide document the command, its JSON schema, and exit codes.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extract reusable production-host conformance checks from the existing fixture suite and expose a side-effect-free check runner for arbitrary executables/config. 2. Add stable JSON/human CLI results and explicit guarded mutation target; test built-in fixtures and sample through command. 3. Document command and run focused/full gates, review, commit, integrate and clean up.
<!-- SECTION:PLAN:END -->
