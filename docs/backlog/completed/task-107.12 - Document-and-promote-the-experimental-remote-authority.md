---
id: TASK-107.12
title: Document and promote the experimental remote authority
status: Done
assignee:
  - '@brett'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-15 05:10'
labels:
  - remote-authority
dependencies:
  - TASK-107.11
references:
  - README.md
  - CHANGELOG.md
  - docs/cli-reference.md
  - docs/mcp.md
  - cmd/worklease-doc-test
  - skills/worklease-workflow/SKILL.md
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: medium
type: docs
ordinal: 144000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Document the measured implementation and label the validated local capability experimental after the real-host harness passes. User docs cover profiles, enrollment, CLI, and MCP. Operator docs cover one process and one namespace per SQLite authority, stop-before-start changes, a single-host filesystem, optional asynchronous backup, hosted locks, offline initialization and restore, selected backup cutoffs, unknown lost-history bounds, pending and installation evidence, indefinite recovery when evidence is missing, reopening attestations, retirement, and the lack of high availability. Remote `replace-file`, provider execution, recovery import, completed-history journaling, and cross-host transfer remain unsupported.

Update the design and normative contract to describe only measured shipped behavior. Document that the binary permanently opens no listener and makes no network request unless the user explicitly invokes remote profile management, selects a remote profile, or runs `serve`; local reads remain setup-free. Public publication, tags, pushes, and release execution remain separate owner-authorized actions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Remote setup, CLI, MCP, and operator guides document the implemented commands, flags, reasons, roles, configuration restart procedure, hosted lock, restore and reopen workflow, private evidence handling, and explicit unsupported operations; `cmd/worklease-doc-test` validates the new guides.
- [x] #2 The recovery runbook requires independent active, retired, and ephemeral installation inventory; enumerable pending sets or equivalent verified coverage; retained and lost-tail outcomes; provider and executor cessation even after terminal receipts or empty pending state; selected durable backup cutoff; the interval through old-authority cessation; unknown bounds; and indefinite recovery when coverage is missing.
- [x] #3 The runbook explains that a fully missing completed operation may remain an attested history gap only with independent evidence of no residual effect, and that recovery import and a completed-history journal remain deferred until a drill shows required evidence or a recovery target cannot be established.
- [x] #4 The design, contract section 20 amendment, section 17 entry, TASK-85 comment, README, CHANGELOG, CLI reference, MCP guide, and `skills/worklease-workflow` describe the same measured experimental capability, retain the deferred follow-ups and triggers, and leave the provider-neutral coordination contract unchanged.
- [x] #5 Release documentation states that the standard binary opens no listener and performs no network request unless remote profile management, a selected remote profile, or `serve` is explicitly invoked; local reads remain setup-free.
- [x] #6 Release artifacts are built and smoke-tested on matching target runners for every supported target, with measured binary-size deltas and no claim that cross-architecture artifacts were executed on the build host.
- [x] #7 The real-host harness report is linked with its measured results and remaining limits. No documentation claims high availability, fencing, provider cessation from completion, or recovery reliability beyond that evidence.
- [x] #8 The validated local documentation and artifacts label the capability experimental. Public publication, tags, pushes, and release execution occur only under separate owner authorization.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Rewrite the remote authority design/operator guide around the measured experimental implementation, recovery evidence requirements, unsupported operations, and real-host harness limits.
2. Align README, changelog, CLI and MCP guides, workflow skill, contract amendments, and TASK-85 history with the same measured behavior while preserving provider neutrality.
3. Extend cmd/worklease-doc-test to enforce the shipped remote documentation contract.
4. Build and smoke-test supported release targets on matching runners, record size deltas and evidence without overstating cross-architecture execution.
5. Run focused documentation checks and all repository quality gates, independently verify acceptance, then finalize the task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Rewrote the remote setup/operator guide and aligned README, changelog, CLI, MCP, workflow skill, and contract around the measured experimental capability. Added doc-test assertions. Corrected remote GC documentation to match the implemented flags. Recorded the TASK-85 amendment comment. Matching-runner release evidence: Actions run 34915583460 passed all four archive/native-smoke jobs at 7e4cef4; measured archive/binary deltas versus v1.2.0 are recorded per target.

Final verification: mise run doc-test parsed the documented server YAML through server.LoadConfig and passed all runnable examples; mise run lint, format-check, test, typecheck, and ci passed. The final CI run included race, govulncheck, built-binary E2E, remote development smoke, docs, and man generation. Independent reviewer findings were fixed (GC flags, unknown-bound wording, quoted YAML, fd durability wording, parsed config validation), and independent acceptance verification passed AC1-AC8 and DOD1 with no findings.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Documented and promoted the implemented remote authority as an opt-in experimental capability across the operator guide, README, changelog, CLI, MCP, workflow skill, and normative contract. Added executable documentation validation for the remote contract and canonical server configuration; recorded five-group local/Lima harness evidence, matching-runner four-target artifact smoke, measured v1.2.0 size deltas, recovery evidence boundaries, deferred triggers, and explicit no-publication limits. Verified with all repository gates and independent acceptance review.
<!-- SECTION:FINAL_SUMMARY:END -->
