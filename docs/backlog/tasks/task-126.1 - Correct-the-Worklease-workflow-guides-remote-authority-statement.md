---
id: TASK-126.1
title: Correct the Worklease workflow guide's remote-authority statement
status: Done
assignee:
  - '@executor'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 20:59'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies: []
references:
  - docs/backlog/docs/worklease-workflow/doc-1 - Worklease-Workflow.md
  - skills/worklease-workflow/SKILL.md
  - docs/remote-claim-authority.md
  - docs/claim-model.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-126
priority: medium
type: docs
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The "Boundaries" section of the Backlog doc `doc-1 - Worklease-Workflow` says: "The local authority is the only shipped authority ... The remote-authority design document is explicitly deferred. Worklease has no HTTP backend or remote fallback." The experimental remote authority shipped in TASK-107 and TASK-109. AGENTS.md tells every agent to read this guide, so the stale text misleads agents now. skills/worklease-workflow/SKILL.md ("Experimental remote authority operations") already describes remote operations correctly and is the source of truth for the wording.

Edit Backlog records only through the Backlog CLI (`backlog doc update`), never by editing the Markdown file directly. Keep the statement that there is no automatic remote-to-local fallback, which remains true (plan D25).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 doc-1 accurately describes the default local authority and the experimental, explicitly selected remote authority, and links docs/remote-claim-authority.md
- [x] #2 `rg -n 'explicitly deferred|only shipped authority|no HTTP backend' docs/backlog/docs` returns nothing
- [x] #3 doc-1 still states that an unavailable remote authority never falls back to local
- [x] #4 No statement in doc-1 contradicts docs/claim-model.md or skills/worklease-workflow/SKILL.md
- [x] #5 The change was made with `backlog doc update`, and `backlog doc view doc-1 --plain` succeeds
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run doc-test` passes and every changed relative link resolves
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace the stale authority paragraph in doc-1 using backlog doc update, matching the skill and remote-authority docs. 2. Preserve the no-fallback guarantee and link the remote-authority documentation. 3. Verify consistency, links, and doc-test; record evidence and finalize through Backlog CLI.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Updated doc-1 via backlog doc update. Verified the experimental explicit remote authority and no-fallback wording against SKILL.md, remote authority guide, and D25; rg found no stale phrases; backlog doc view succeeded; mise run doc-test passed and changed links resolve.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Corrected doc-1 to describe local default and explicitly selected experimental remote authority with no automatic fallback. Verified consistency, stale wording scan, CLI view, links, and mise run doc-test.
<!-- SECTION:FINAL_SUMMARY:END -->
