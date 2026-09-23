---
id: TASK-127
title: Request bulk dependency fields from Backlog.md upstream
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 21:13'
labels:
  - work-queue
  - backlog-md
  - reviewed
milestone: m-1
dependencies:
  - TASK-126.6
references:
  - 'https://github.com/MrLesk/Backlog.md'
  - 'https://github.com/MrLesk/Backlog.md/issues/1035'
documentation:
  - docs/work-queue-tui-proposal.md
priority: medium
type: task
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`backlog task list --json` and `search --json` omit dependencies, so a complete dependency graph costs one `task view --json` process per task (plan section 3, D13). If list JSON included `dependencies`, and ideally a per-task content version, the full graph would take one process, and most of the TASK-129.4 edge cache would become a fallback for older versions. The plan runs this request in parallel with the slices, so it gates nothing.

This is external communication on the user's behalf. Draft it with the writer agent under the user-voice skill, and post only after the user explicitly approves the final text. Check the latest Backlog.md release first. If it already provides the field, record the version instead of filing.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The latest Backlog.md release notes and `task list --json` output were checked, and the version is recorded
- [x] #2 If the field is missing, the writer agent produced a draft upstream issue that cites the TASK-126.6 measurements and requests `dependencies` (and a per-task content version) in `task list --json` and `search --json`
- [x] #3 The issue is posted only after explicit user approval, and its URL is recorded in this task and in plan section 14
- [ ] #4 If the field already exists, plan sections 3 and 14 and D13 are updated with the supporting version instead
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Verify latest release and JSON fields. 2. Draft an upstream request using TASK-126.6 measurements and seek explicit posting approval. 3. Post only if approved; record URL, update proposal, validate, commit and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Backlog.md 1.52.0 is the latest release; its notes say JSON fields unchanged. Local 1.52.0 task list JSON omits dependencies and per-task content version.

Writer draft prepared, not posted. Title: Include dependencies and content version in task list and search JSON. Body requests both fields in list/search JSON and cites v1.52.0, 10k-task list p50 2.049s, view p50 1.975s, and >=82-minute full scan at four ideal concurrent processes on Apple M1 Max/32 GiB. Awaiting explicit approval to post exact draft.

User explicitly approved the exact writer draft. Posted upstream issue #1035; gh issue view confirms title/body and OPEN state. Section 14 now links the issue in task-127-upstream-dependencies worktree. AC #4 is conditional and not applicable because v1.52.0 lacks both fields.

Verification: gh release view v1.52.0, backlog --version and list JSON; writer draft delivered and explicitly approved; gh issue view 1035 matches posted text; proposal section 14 in worktree links issue. Worktree checks: git diff --check, mise run lint, format-check, test, typecheck passed. AC #4 is the false branch of the conditional.

Worktree commit b91a847 passed lint, format-check, test, typecheck, hooks. Review: one scoped pass of the one-line proposal change and gh issue body; no item-scoped defects. Integration remains pending: primary main has concurrent unrelated uncommitted edits to docs/work-queue-tui-proposal.md and other files, so merging now would disturb another worker. Next: after main is clean, merge task-127-upstream-dependencies, commit the provider task file from primary checkout without staging unrelated files, verify section 14 and issue URL on main, then finalize Done and clean owned worktree.

Integrated b91a847 into main at cb008a9. Proposal section 14 retains the measured 82-minute bound and links the verified open upstream issue #1035. Full lint, format-check, test, typecheck, doc-test and staged hooks passed. AC4 is inapplicable: the fields are absent in Backlog.md 1.52.0.

Post-completion review: issue #1035 still OPEN with no replies; Backlog.md 1.52.0 remains latest. Proposal §14 wording changed from 'measured at least' to 'projected' ~82 min. No follow-up; revisit when upstream responds.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Filed approved upstream request #1035 and integrated the proposal link; verified issue state and full project gates on main.
<!-- SECTION:FINAL_SUMMARY:END -->
