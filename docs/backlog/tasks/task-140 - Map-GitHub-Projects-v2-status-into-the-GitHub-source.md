---
id: TASK-140
title: Map GitHub Projects v2 status into the GitHub source
status: Done
assignee: []
created_date: '2026-09-25 16:26'
updated_date: '2026-09-25 20:24'
labels:
  - work-queue
milestone: m-1
dependencies: []
documentation:
  - docs/work-queue-tui-proposal.md
  - skills/worklease-workflow/references/source-providers/github-issues.md
priority: medium
type: feature
ordinal: 62000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub Issues has only open/closed, so the GitHub adapter cannot represent In Progress, Blocked, or In Review, and Start work on a GitHub source can only claim (plan §8). Teams that run their workflow on a GitHub Projects v2 board keep that state in a single-select Status field (or similar) on the project item, which the queue ignores today. Plan §17 deferred this for S6 because it needs the `project` token scope and per-project field discovery. The user asked for it on 2026-09-25 during TASK-134 (S8 intake).

This extends the existing built-in GitHub adapter; it is not a new provider and does not change GitHub claim keys (D24). An issue can belong to several projects and a project can hold items from many repositories, draft issues, and pull requests, so the binding must name exactly one project and field. Projects are owned by a user or organization, independently of the repository.

Reversing the §17 deferral means updating §17, D2/D14 as needed, and the §7 GitHub column in the same commit as the decision. GHES support for Projects v2 is unprobed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 docs/work-queue-tui-proposal.md records the decision in §2, §16, and §17 (replacing the S6 deferral), and a §3 evidence subsection from a live probe on a user-approved synthetic project: field and option discovery, item pagination, whether field changes advance issue or item `updatedAt`, an issue in multiple projects, draft and pull-request items, required token scopes, and rate-limit cost.
- [x] #2 A GitHub source can bind to exactly one Projects v2 project (owner plus project number, with the node ID recorded) and one single-select status field in queue.yaml; the binding is validated by discovery and a changed or missing project, field, or option disables the mapping with a structured diagnostic rather than inferring state.
- [x] #3 Configured field options map to queue workflow states (for example In Progress, Blocked, In Review, Done); unmapped options stay raw, and the raw option name is shown beside the normalized state.
- [x] #4 Issues outside the bound project, draft issues, and pull-request items are handled explicitly: drafts and pull requests never become claimable issues, and an issue without a project item reports its state as unmapped, not free or ready.
- [x] #5 Readiness and completion still follow D27: a project Done option does not override issue state, and conflicts between issue state and project status are shown rather than silently resolved.
- [x] #6 Setup requests read-only project scope first; status writes require explicitly enabled write scope, and capabilities are rediscovered after scope changes.
- [x] #7 Start work and focused state edits can set the mapped project field through the §8 recovery pipeline with read-back of the exact item and field value; the transition is unconditional and declared as such.
- [x] #8 GitHub claim keys and existing sources without a project binding behave exactly as before, verified by the existing identity vectors and tests.
- [x] #9 Tests cover option rename or deletion, an issue in multiple projects, project permission loss, pagination interruption, and lost write responses.
- [x] #10 User-facing queue docs describe the binding, required scopes, and limits.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Probe a user-approved synthetic Projects v2 board and record API/scope evidence. 2. Extend GitHub queue config, discovery/sync/state/readiness and recoverable writes while preserving unbound behavior. 3. Add focused tests and user-facing docs; run focused race and repository gates, review once, commit, merge and clean verified worktree; finalize task on primary checkout.

Integration adjustment: main added Linear `project` UUID and shared queue-source options while this branch was in flight. Use a distinct `githubProject` binding key in queue.yaml, preserve Linear project string and credential-helper options, then rerun merged gates before delivery.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Live synthetic probe approved. gh read:project was granted first; project write scope granted separately. Created private projects #5 (PVT_kwHOAT55tM4BksRF) and #6 (PVT_kwHOAT55tM4BksRN), synthetic issue worklease#6, draft and pre-existing closed PR#2 as project items (PR content unchanged). Field Workflow Probe PVTSSF_lAHOAT55tM4BksRFzhjb-Hs has 4 discovered option IDs. items(first:2) returned totalCount=3, hasNextPage and a valid second cursor. Issue #6 belongs to both projects with distinct item IDs. Setting status In Progress advanced project item updatedAt 17:29:48Z -> 17:30:13Z but issue updatedAt remained 17:29:33Z. Read and write GraphQL queries each reported rateLimit.cost=1. Fixture cleanup outstanding after tests.

Cleaned approved fixtures: verified exact project IDs/titles #5/#6, deleted both; verified synthetic worklease#6 and closed it as not planned with cleanup comment. Existing PR#2 was never changed (only added to deleted project). Existing unrelated projects #3/#4 remain.

Branch 7d0c5e3 implements optional Projects status; full gates, focused race, doc-test and staged hooks passed before main merge. One general review found four concrete defects (terminal dependency/advisory status, recovery after disabling writes, resumed status projection, scan-map eviction); corrected with focused tests. Main advanced concurrently with Linear/Beads/credential-helper work; integrating with distinct githubProject config key to avoid collision.

Verification on merged main 3ce36d7: mise run lint, format-check, GOFLAGS=-p=1 -parallel=2 mise run test, mise run typecheck, and mise run doc-test passed. Focused new/changed tests passed go test -race -count=3 in internal/queue (TestGitHubProject*), internal/cli (source options/Start work), internal/config (binding validation), and internal/queueui (raw/conflict display); resource TestVersionedKeyVectors and TestStaticPolicyGoldenDerivations passed. Staged mise run hooks and commit hooks passed in worktree. Scope rediscovery tested using X-OAuth-Scopes and read-only/write tokens; GraphQL fieldValueByName confirmed by live schema introspection. Review: one general pass identified four item-scoped defects and a TUI resolve omission, all corrected with tests; no further general pass. Branch implementation 7d0c5e3 merged through 714b592 to main 3ce36d7; main later pinned Beads tool separately. Verified owned clean worktree/ancestor via wt list and creation receipt, then wt remove deleted worktree and branch; synthetic projects and issue were cleaned earlier. No push/PR. GHES and large-scale multi-client project quota remain unprobed, explicitly documented limitations, not acceptance blockers.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Mapped one explicit GitHub Projects v2 status field into the existing GitHub queue source, with fail-closed discovery, independent project refresh, raw/conflict display, scope-gated unconditional Start/focused writes and exact recovery read-back; legacy issue identity unchanged. User-approved synthetic probe and cleanup recorded in §3. Merged 3ce36d7 to main, all required gates, doc-test, focused race and identity vectors passed; reviewed and corrected five concrete issues, worktree removed.
<!-- SECTION:FINAL_SUMMARY:END -->
