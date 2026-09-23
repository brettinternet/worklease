---
id: TASK-97
title: Clarify remote authority value and shared coordination design
status: Done
assignee:
  - '@codex'
created_date: '2026-09-12 23:08'
updated_date: '2026-09-12 23:13'
labels: []
dependencies: []
references:
  - docs/distributed-cloudflare-claim-authority.md
type: docs
ordinal: 122000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Evaluate whether remote Worklease earns adoption beyond trackers and lock primitives before authorizing implementation. Capture shared identity/configuration and remote ledger behavior, operational risks, and what is designed versus still unresolved.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Proposal compares trackers, schedulers, Redis/database locks and identifies validation criteria and non-goals.
- [x] #2 Proposal specifies remote identity/configuration agreement and migration safety without changing local v1 behavior.
- [x] #3 Proposal explains remote ledger recovery, authorization, retention, watches and external-effect limits.
- [x] #4 Proposal distinguishes existing design coverage, new requirements and unresolved pre-deployment decisions; required repository checks pass.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Compare the deferred proposal with the current resource, ledger, recovery and remote-boundary contracts.
2. Extend only the deferred design with product validation, proposed shared configuration/identity and remote ledger requirements, plus explicit unresolved decisions and acceptance scenarios. No remote implementation or local contract change.
3. Independently check design consistency and coverage, run repository checks, finalize this task and commit only the proposal and task record.

The configured writer model was rejected before any child launched, and persisted workflow resume does not support a model override. Complete the wording and contract/scenario review directly; do not claim independent review.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Manual acceptance review against Go Product Contract sections 7, 11 and 20 and internal/ledger/ledger.go confirms: AC1 compares trackers/schedulers/Redis/SQL/etcd and defines private-deployment demand validation; AC2 specifies trusted bootstrap, immutable catalog keys, revision-checked admission, replay across manifest changes, recovery-only migration admission and no offline fallback; AC3 specifies transactional remote ledger, namespace-authorized redacted reads, epoch-authenticated private inspection, transitive predecessor recovery, cursor gaps and retention backpressure; AC4 separates existing written guarantees, proposed requirements and unresolved operational choices, with six future executable scenario groups. These are design requirements, not remote implementation evidence.
Validation passed: mise run lint, mise run format-check, mise run test, mise run typecheck, backlog doctor, mise run hooks-install, and git diff --check. Independent drafting/review workflow did not launch because writer model was outside modelScope; review was completed directly. During concurrent checkout activity core.bare became true; restored false so normal worktree operations function again. Preserved unrelated task and AGENTS.md edits.

Staged-document validation: mise run hooks succeeded (Go jobs correctly selected no files for this documentation-only change); git diff --cached --check passed. Only the proposal and TASK-97 record are included in the commit.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Expanded the deferred remote proposal with product differentiation and validation, shared identity/configuration consistency, remote ledger recovery and privacy, operational decisions, and future release scenarios. Checked against the current local contract and passed lint, formatting, tests, type checks, Backlog integrity and diff checks. No remote code or local contract changes.
<!-- SECTION:FINAL_SUMMARY:END -->
