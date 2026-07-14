---
id: TASK-9
title: Correct lifecycle and guarantee documentation
status: In Progress
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:33'
updated_date: '2026-07-14 04:41'
labels:
  - documentation
  - cli
  - workflow
dependencies: []
references:
  - README.md
  - skills/worklease-workflow/SKILL.md
priority: high
type: docs
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the README a runnable and accurate guide to the released Worklease lifecycle and guarantees. Cover existing behavior without implementing TASK-4 checkpoint metadata, TASK-6 unknown-operation reconciliation, or TASK-7 verbose diagnostics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 README provides a complete CLI lifecycle from resource derivation and acquisition through guarded work, authoritative provider checkpoint verification, and release, with every referenced claim value defined.
- [ ] #2 Documentation states that heartbeat and completed exec/replace-file operations advance the active revision, while release validates and consumes the latest revision without advancing it.
- [ ] #3 Documentation distinguishes Worklease lease/operation receipts from caller- or provider-owned durable checkpoints and does not imply that the current CLI has a checkpoint command.
- [ ] #4 Documentation explains coordination-only claims, ownership-loss behavior, unknown outcomes, state-home selection, exact resource identity, token handling, and the boundary between local coordination and provider fencing.
- [ ] #5 README includes concise recipes for singleton commands, scarce local resources, local-agent item ownership, source-wide Markdown replacement, idempotent replay, and locally coordinated remote-provider writes; focused checks validate commands, links, and terminology.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Runnable lifecycle and guarantee guide (AC1-AC5).** Rework README lifecycle and recipes against the released CLI, defining every claim value and revision transition from key/acquire through heartbeat or guarded work, caller-owned durable checkpoint verification, and release. Update workflow guidance only where its generic invariants need the same terminology. Bundle focused command-help, link, and terminology checks with the documentation change; do not implement checkpoint, reconciliation, or verbose-diagnostic behavior.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now. Documentation must describe only commands present in the released baseline under test; concurrent future features remain out of scope.

**Goal and target area:** Update existing `README.md` and, only for generic invariant terminology, `skills/worklease-workflow/SKILL.md`. No new runtime code or provider behavior.

**Resolved decisions:** The lifecycle example derives one resource, acquires and captures claimId/token/revision, heartbeats or performs guarded work using the current revision, verifies a caller/provider-owned durable checkpoint outside Worklease, then releases using the latest revision. Heartbeat and completed exec/replace-file advance revision; release consumes the latest revision without increment. Worklease operation receipts are coordination evidence, not authoritative provider checkpoints. Examples must distinguish normal local guarded operations from coordination-only remote-provider writes and must not claim a current checkpoint/reconciliation/verbose-status command. Direct token examples acknowledge process-list risk until TASK-13 exists.

**Non-goals:** implementing or documenting unreleased TASK-4/6/7 surfaces, changing CLI behavior, inventing provider APIs, or claiming cross-host/provider fencing.

**Evidence and assumptions:** README and workflow skill exist; CLI help and current tests are the executable source of truth. Recipes use explicit placeholders and never embed live credentials.

**Task/acceptance map:** T1→AC1-5.

**Pending verification:** execute every documented local command against a temporary home where safe; run command-help/package smoke tests; check internal links and forbidden terminology.

**Next action:** inventory released CLI help, then rewrite the lifecycle in one coherent documentation pass.

**Refinement checkpoint:** refined: TASK-9 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=ebaa9c09-b189-40f3-bfff-ba59a6534bde; claimRevision=6; refinement: complete.

Implementation checkpoint (T1 runnable lifecycle and guarantee guide): commit e376ee83fe4118ac307752dc4d6bc21275d2910b on isolated branch rewrites README lifecycle/CLI contract/recipes/guarantee boundary and adds workflow checkpoint terminology. Smoke-tested key, acquire, checkpoint, exec, and release with /tmp/worklease-task9-smoke; verified checkpoint/exec revision advances and release consumes latest revision. mise run lint, format-check, test (72 tests), typecheck, and hooks passed. T1 complete. Next task: review complete accumulated TASK-9 implementation. Remaining acceptance: review evidence. Executable baseline conflict: checkpoint is a released coordination-metadata command; docs distinguish it from provider-durable checkpoint state.
<!-- SECTION:NOTES:END -->
