---
id: TASK-126.6
title: Measure Backlog.md at scale and probe its Git side effects
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 05:22'
labels:
  - work-queue
  - backlog-md
milestone: m-1
dependencies: []
references:
  - 'https://github.com/MrLesk/Backlog.md'
  - backlog.config.yml
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-126
priority: high
type: spike
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan section 3 measured Backlog.md 1.52.0 on 102 tasks at about 0.6 s per `task list --json` and 0.5 s per `task view --json`. D21 targets 10,000 items per source. The dependency edge cache (TASK-129.4) and the viability of next-ready (TASK-130.4) both depend on whether per-task view cost grows with project size. Plan section 5 also flags unverified Git side effects. `auto_commit` may commit unrelated staged changes and run hooks, and `remote_operations` or `check_active_branches` may contact Git remotes during reads.

Build fixtures only in scratch projects under the OS temporary directory, and never touch this repository's docs/backlog. A generator may write task files from a template captured from a CLI-created task, but the Backlog CLI must accept the result: list JSON returns every task. Measure on the D22 reference machine (Apple M1 Max, 32 GiB); if you are on different hardware, record it and ask the user whether to proceed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A committed, deterministic fixture generator (reused later by TASK-129.6) produces Backlog.md projects with 1,000 and 10,000 tasks and realistic dependency chains. `backlog task list --json` returns every generated task
- [x] #2 The task records p50/p95 wall time and peak RSS for `task list --json` and `task view --json`, plus the change-to-emit latency of `task list --json --watch`, at 102, 1,000, and 10,000 tasks, along with the Backlog.md version and machine
- [x] #3 The results state whether `task view --json` cost grows with project size, and project the time for a full edge scan at 10,000 tasks with 4 concurrent processes
- [x] #4 In a scratch Git repository, the task records whether an edit with `auto_commit: true` commits unrelated staged changes, whether hooks run, and how `bypass_git_hooks` changes that
- [x] #5 The task records whether reads with `remote_operations: true` or `check_active_branches: true` contact a remote, observed with GIT_TRACE or a remote URL that cannot resolve
- [x] #6 Plan section 3 and section 14 (Backlog.md loading) are updated with the results, including whether next-ready over large projects must wait for the upstream bulk-dependency field
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Generate deterministic temporary Backlog projects and benchmark list/view/watch at 102, 1,000, and 10,000 tasks on the reference machine. 2. Probe scratch Git auto-commit/hooks and remote-contact behavior. 3. Record measurements and decisions in the task and proposal; run quality gates, commit, integrate, and verify.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Scale probe on Apple M1 Max/32 GiB, Backlog.md 1.52.0, five samples. 102/1,000/10,000 tasks: list p50/p95 s 0.259/0.270, 0.439/0.453, 2.049/2.123; peak RSS p50/p95 MiB 88.1/88.8, 149.0/149.1, 272.8/275.4. View p50/p95 s 0.258/0.260, 0.406/0.408, 1.975/2.376; RSS 86.7/87.5, 144.8/147.1, 301.1/302.9. Watch title-change to full JSON p50/p95 s 0.085/0.088, 0.266/0.278, 3.021/3.323. List returned all generated records. View grows ~7.7x; ideal four-process scan >=82 min at 10k. Scratch Git: auto-commit excluded unrelated staged file, ran pre-commit; bypass disabled hook. In tested list/view reads, neither remoteOperations nor checkActiveBranches caused Git trace output with nonresolving SSH remote; scope of inference limited to these commands.

Validation: generator list JSON cardinalities 102/1,000/10,000; five-sample benchmark results above; scratch Git trace/commit/hook probes; mise run lint, format-check, test, typecheck, doc-test, hooks and Python py_compile passed in isolated worktree. Review: one general pass; fixed watch pipe buffering and title mutation handling during focused checks; no remaining item-scoped defects. Worktree commit f85cc77.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Measured Backlog.md scale and scratch Git effects; documented 82-minute best-case cold edge scan and bulk-field dependency. Verified fixture cardinalities, watch timing, Git hook/staging behavior, and all required checks; commit f85cc77.
<!-- SECTION:FINAL_SUMMARY:END -->
