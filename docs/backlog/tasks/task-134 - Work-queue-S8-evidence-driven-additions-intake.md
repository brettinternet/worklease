---
id: TASK-134
title: 'Work queue S8: evidence-driven additions intake'
status: Done
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 23:07'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies: []
references:
  - TASK-142
  - TASK-139
  - TASK-141
  - TASK-140
documentation:
  - docs/work-queue-tui-proposal.md
priority: low
type: spike
ordinal: 46000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan section 16 (S8) lists additions that each require their own decision backed by evidence: more providers (Beads, Linear, Jira, or GitLab, by demand), a native-authority study under the plan section 9 admission criteria, and a separately authenticated source service once duplicated traffic, latency, and authorization needs are measured. None of them is justified yet. This task is the intake gate: it gathers evidence and turns each accepted addition into its own task, instead of building anything speculatively.

Blocked until evidence exists: a concrete request for a named provider, a recorded measurement of duplicated source traffic, latency, or authorization need, or a native-authority requirement. Remove the Blocked label only when citing that evidence. A native-authority study or source-service decision does not wait for S7. Any accepted addition implemented as an external adapter must depend on the S7 parent task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 For each candidate (Beads, Linear, Jira, GitLab, a native-authority study, a source service), evidence of demand or measured need is recorded, or its absence is stated
- [x] #2 Each candidate decision (accept, defer, or reject, with rationale) is recorded in plan sections 2 and 16 before intake completion. Pre-existing user-requested tasks TASK-139, TASK-140, and TASK-141 are documented as ordering exceptions; subsequent tasks are created only after the decision is recorded
- [x] #3 Each accepted candidate gets its own backlog task with acceptance criteria, linked from this task. External-adapter implementations depend on the S7 parent task
- [x] #4 The task is not completed with every candidate deferred for lack of evidence; it stays open and Blocked instead
- [x] #5 A native-authority study, if accepted, is scoped against every plan section 9 admission criterion
- [x] #6 A source service, if accepted, cites the measured traffic, latency, and authorization evidence that justifies it
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run doc-test` passes and every changed relative link resolves
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Intake decision (evidence: user request 2026-09-25, "add Linear support now since I use it frequently in one of my environments"): accept Linear; accept Jira Cloud with API tokens (user request 2026-09-25, TASK-139); accept GitHub Projects v2 status mapping, reversing the §17 S6 deferral (user request 2026-09-25, TASK-140); accept Beads (user request 2026-09-25, TASK-141); defer GitLab (no request), native-authority study (no requirement; Linear exposes no native claim, to be confirmed by probe), and source service (no traffic/latency/authorization measurement).

User-confirmed choices (2026-09-25): built-in Go adapter; first parent covers probe, read, sync, and claims with focused writes as later subtasks under the same parent; credential via a user-configured helper argv; live probe against a dedicated Linear test team where synthetic issues, relations, and moves are allowed and cleaned up.

1. Docs first (one commit, AC #2, DoD #2), in docs/work-queue-tui-proposal.md:
   - §2: amend D2 (initial sources plus built-in Linear, accepted in S8) and add D29: Linear adapter speaks GraphQL over HTTPS directly; credential comes from a configured helper argv, held only in memory, and `viewer` is verified before every write and on credential change; claims use the existing static `linear` key policy with source = Linear organization ID and item = issue UUID; the `ENG-123` identifier and team are display/locator only, so a team move is not an identity change; requests are serial per organization/account and honor Linear's request and complexity limits.
   - §3: Linear evidence subsection placeholder, filled by the probe subtask.
   - §7: Linear column in the initial declarations table (probe-pending values are marked unknown).
   - §10 credential row and §11 credential-helper row: the generic helper ships with Linear; `gh` stays built-in.
   - §16 S8 row and §17: record each candidate's accept/defer decision and evidence (Linear, Jira Cloud, GitHub Projects v2, and Beads accepted; replace the §17 Projects v2 deferral; Jira Data Center and OAuth deferred, OAuth tracked in DRAFT-14).
   - Update skills/worklease-workflow/references/source-providers/linear.md with the pinned locator and item form.
   - Run `mise run doc-test` and check links.
2. Create the Linear parent task (milestone m-1, label work-queue, linked from TASK-134), with subtasks in dependency order:
   a. Probe Linear API on the test team; record in §3 and revise D29/§7 if contradicted. Cover viewer/org/team IDs, Relay pagination limits and ordering, the `updatedAt` filter, whether relation add/remove bumps `updatedAt` on either endpoint, archive/trash/permission-loss visibility, team move (identifier changes, UUID stable), workflow state types, rate-limit and complexity headers, single-assignee semantics, comment Markdown round-trip for the operation marker, and absence of native claims. Mutations only on synthetic issues, which are cleaned up.
   b. Generic credential helper: `queue.yaml` argv, bounded runtime/output, scrubbed env, redacted stderr, token memory-only, per-credential serialization.
   c. Linear identity vectors in internal/resource/testdata (no policy byte change); queue- and CLI-derived keys byte-equal.
   d. Read-only Linear adapter: config (`adapter: linear`, organization, team, optional project filter, account), resolve, capabilities, list, readItems, readDependencies (`blocks` = hard edge; related/duplicate/similar/parent informational), state-type mapping, raw state kept; passes the shared conformance suite.
   e. Incremental sync and reconciliation in the queue index with the per-quota scheduler; relation reconciliation per probe findings; partial scans never advance the watermark or prove deletion; D21 scale benchmark on fixtures.
   f. Claims and selection: Claim for me, D11, identity gate, `queue next --claim`, MCP `queue_next` on Linear sources; remote admission under `coordination:`.
   g. Focused writes (depends on a–f): configured state transitions including Start work, comment with operation marker, assign-to-me with declared single-assignee replace semantics and an explicit confirmation when someone else is assigned; full recovery/read-back per §8.
3. Finalize TASK-134: verify AC #1–#6 (Linear, Jira, Projects v2, and Beads accepted and linked; the others deferred with evidence absence stated; #5/#6 not applicable), then move to Done. A future named request opens a new intake.

2026-09-25 scope update: user also requested Beads (TASK-141) and GitHub Projects v2 status (TASK-140). Record D31/D32 and S8/§17 decisions in the docs commit; Beads is accepted, GitLab alone remains deferred. The Projects extension has its own task, without changing GitHub claim identity. Probe evidence belongs to follow-up implementations, not this intake.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Blocked label removed: concrete named-provider request for Linear from the user on 2026-09-25.

Follow-up: TASK-139 (opt-in OAuth for source providers). When the Linear parent and credential-helper subtask are created, add them as TASK-139 dependencies.

Correction: the OAuth follow-up was demoted to DRAFT-14 (revisit later); TASK-139 is now the Jira Cloud API-token adapter. When the Linear credential-helper subtask exists, add it as a dependency of TASK-139 and DRAFT-14.

AC #2 ordering: TASK-139 was created at the user's request before the Jira decision was recorded in plan §2/§16. The step-1 docs commit must record it before TASK-134 closes.

TASK-140 (GitHub Projects v2 status) and TASK-141 (Beads) created at the user's request before the plan §2/§16/§17 records; the step-1 docs commit must record them before TASK-134 closes.

Scope refresh: TASK-140 and TASK-141 were created from separate user requests while intake documentation was being drafted. Both are accepted S8 additions; update the plan and docs accordingly. Live Linear API key is offered for the subsequent probe, not needed for intake.

Intake docs commits 5ded9ad and a9a69b4 on task-134-intake: D29–D33, S8 candidate decisions and §17 Projects reversal; TASK-142 with seven ordered children created. TASK-139 and DRAFT-14 depend on TASK-142.2 credential helper; TASK-140 and TASK-141 came from user requests. All are built-in additions, so the S7 external-adapter dependency condition does not apply. No Linear token needed until TASK-142.1 probe.

User approved amending AC #2 to recognize that TASK-139/140/141 preceded the committed plan decision; D29–D33 and §16 are committed on main at 452777a before TASK-134 completion. TASK-142 was created after the first decision commit 5ded9ad.

AC verification on main 452777a: §2 D29–D33 and §16/17 explicitly accept Beads, Linear, Jira Cloud and Projects v2 on named user requests, defer GitLab/native study/source service for absent evidence, and scope Jira Data Center/OAuth separately. TASK-142 and seven child tasks have acceptance criteria and dependency graph; TASK-139/140/141 exist with criteria, linked from TASK-134 references. All accepted additions are built-ins (no external-adapter S7 prerequisite). AC #4 satisfied by accepted demand; AC #5/#6 conditional and inapplicable because those candidates are deferred. User-approved AC #2 exception records the three pre-existing tasks. DoD: doc-test passed on merged main, relative links in both changed Markdown files resolve; decision/register, capability declarations, auth and §17 deferral were revised in documentation commits merged at 452777a. Full worktree lint, format-check, test, typecheck passed; staged hooks passed; one docs consistency review corrected S8 proposal/shipping labels. No live Linear probe in this intake.

TASK-143.2 (host-resolved credentials for external adapters) should depend on the Linear credential-helper subtask once it exists, alongside TASK-139 and DRAFT-14. External adapter author tooling tracked under TASK-143.

Correction: TASK-143.2 now depends on TASK-142.2 (credential helper exists).

Post-merge main validation at 452777a: mise run lint, format-check, test, typecheck, doc-test all passed. Remaining work belongs to TASK-142.1 (securely obtain offered Linear API key and probe dedicated test team), then TASK-142.2–.7; Jira TASK-139 awaits the helper. No intake blocker.

Post-completion review: D29–D33 and §16/§17 S8 decisions present; accepted additions TASK-139/140/141/142 exist with criteria; GitLab, native-authority study, and source service deferred. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
S8 intake accepted Linear, Jira Cloud, Beads, and GitHub Projects v2 based on named user requests; deferred GitLab, native authority, source service, Jira Data Center, and OAuth without supporting evidence. Recorded D29–D33 and §16–17, created TASK-142 with seven dependent children, linked all accepted work. Worktree docs merged to main at 452777a; all repository gates, doc-test and relative-link checks passed. One consistency review found and corrected unshipped capability labels. Historical task-before-decision ordering for TASK-139/140/141 was documented and AC #2 amended with user approval; no remaining intake blocker. Next: TASK-142.1 Linear probe.
<!-- SECTION:FINAL_SUMMARY:END -->
