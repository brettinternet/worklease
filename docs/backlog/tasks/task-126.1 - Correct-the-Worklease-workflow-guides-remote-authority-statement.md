---
id: TASK-126.1
title: Correct the Worklease workflow guide's remote-authority statement
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
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
- [ ] #1 doc-1 accurately describes the default local authority and the experimental, explicitly selected remote authority, and links docs/remote-claim-authority.md
- [ ] #2 `rg -n 'explicitly deferred|only shipped authority|no HTTP backend' docs/backlog/docs` returns nothing
- [ ] #3 doc-1 still states that an unavailable remote authority never falls back to local
- [ ] #4 No statement in doc-1 contradicts docs/claim-model.md or skills/worklease-workflow/SKILL.md
- [ ] #5 The change was made with `backlog doc update`, and `backlog doc view doc-1 --plain` succeeds
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run doc-test` passes and every changed relative link resolves
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
