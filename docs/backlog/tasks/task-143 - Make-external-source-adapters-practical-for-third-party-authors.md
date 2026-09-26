---
id: TASK-143
title: Make external source adapters practical for third-party authors
status: Done
assignee: []
created_date: '2026-09-25 16:38'
updated_date: '2026-09-26 01:32'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies:
  - TASK-143.1
  - TASK-143.2
  - TASK-143.3
  - TASK-143.4
  - TASK-143.5
documentation:
  - docs/external-adapter-protocol.md
  - docs/work-queue-tui-proposal.md
  - skills/worklease-workflow/references/external-adapter-authoring.md
priority: medium
type: feature
ordinal: 72000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
S7 (TASK-133) shipped the external adapter protocol: a supervised JSON-RPC 2.0 stdio process with a manifest, explicit executable approval, the static `generic` claim policy, and a host conformance suite. It is the supported way for users to add their own backends; hooks, executable policy plugins, and an event bus stay rejected (D16, D17) because they would split claim exclusion domains and make write recovery unverifiable.

In practice an outside author still cannot build one comfortably: the conformance suite runs only as a Go test in this repository against built-in fixtures, the host passes only an opaque `credentialRef` so each adapter must reinvent secret handling, the only example is read-only, v1 stability is not stated for external authors, and `queue init` cannot configure an external source. The user asked on 2026-09-25 to close these gaps so users can try supporting their own backends.

The user wants the CLI approached with API-driven design: every command added here is a stable, scriptable interface first, with `--json` output in the existing CLI envelope (`schemaVersion`, `ok`, `operation`, and `error.reason`/`exitCode`/`details`), stable diagnostic reasons, and documented exit codes, and the human output is a rendering of that same result.

SDKs and a generic MCP-bridge adapter are out of scope: the protocol is plain JSON-RPC, and MCP tools carry no pagination, capability, or receipt semantics, so each provider still needs its own mapping. Revisit SDKs only on demonstrated demand.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every subtask is Done with its acceptance criteria verified.
- [x] #2 An author outside this repository can follow the authoring guide from an empty directory to an approved, conformance-checked, credentialed, write-capable adapter using only the shipped binary and documentation; this is exercised end to end at least once and recorded in task notes.
- [x] #3 D16/D17 and plan §11 in docs/work-queue-tui-proposal.md reflect the shipped author tooling, and hooks, policy plugins, and SDKs remain explicitly out of scope.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Verify every TASK-143 subtask is Done and inspect the shipped author guide and reference adapter. 2. From an empty directory, build/use only the shipped binary and follow the guide to configure, conformance-check, credential, approve and exercise a write-capable adapter; record exact commands and evidence. 3. Update D16/D17 and plan §11 to match delivered tools, run gates, commit, merge to main and clean up.

4. Repair external-source identity confirmation exposed by the end-to-end exercise; add a focused regression test and rerun validation.

5. Complete the end-to-end CLI write exercise by enabling explicitly mapped external Start work in queue next --claim --start through the existing guarded/recoverable write controller; prove with the disposable credentialed fixture, then rerun gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC1: TASK-143.1–143.5 are Done with 7/7, 6/6, 4/4, 4/4 and 5/5 checked acceptance criteria (backlog task list --plain). AC2: in a fresh owner-private /private/tmp/worklease-task-143-author-OU0R5Q HOME/XDG directory, used a built worklease host binary and separately supplied disposable reference adapter executable (no in-repo runtime imports) to run queue adapter protocol --json (v1), queue adapter check --executable /private/tmp/worklease-task-143-reference --adapter-config JSON --disposable-target reference-1 --json (verdict pass; credential-leak, mutation-receipt, lost-response pass), queue init --adapter external --executable ... --adapter-config JSON --portable-claims fixture/planning --json (manifest and digest, unapproved), queue adapter approve --source worklease.reference.local-fixture --acknowledge --json (approved digest), queue --view Ready identity confirm --source ... --acknowledge (succeeded), queue query --view Ready --json (complete source and eligible claim, principal alice). Added private 0600 credential hash store and executable helper, config.origin https://reference.invalid, account alice and start Doing mapping, reapproved; queue next --view Ready --claim --start --session author-e2e-2 --agent author --json returned acquired=true and transition.outcome=applied, reference fixture shows rawStatus Doing and writeState receipt operation 3c08daf4a9c04cbd40cfca954cf9986b by alice at fixture-v6; queue recovery --json returned [] and release succeeded. This is a network-free simulated provider; real outside authors supply their own executable/provider mapping. Reproduced external init from an empty non-Git working directory in TestQueueInitExternalFailureAndPortableClaims. Discovered and fixed an identity-confirmation panic (unregistered external adapter key), verified by focused CLI regression. AC3: D16/D17 and plan §11 document shipped protocol/CLI, credential channel, approval, receipts and exclusions; guide now contains minimal standalone fixture and credential/Start walkthrough. Validation: mise run lint, format-check, test, typecheck and staged mise run hooks passed; changed behavior race count=3 passed. Review: one item-scoped self-review, no residual actionable defect; independent reviewer unavailable due invalid API key. Initial accidental queue init without isolated XDG created a user config queue.yaml and lock; confirmed both were new and immediately moved to Trash, leaving no user queue state.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed external adapter author tooling, documented the v1 contract, fixed external identity confirmation, and exercised an isolated credentialed reference adapter through conformance, approval, guarded Start and receipt read-back. Full quality gates and focused race tests passed; merged to main (937c8b9, fcc052e, 085cfc4).
<!-- SECTION:FINAL_SUMMARY:END -->
