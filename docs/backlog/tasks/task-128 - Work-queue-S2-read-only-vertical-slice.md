---
id: TASK-128
title: 'Work queue S2: read-only vertical slice'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 19:47'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-128.1
  - TASK-128.2
  - TASK-128.3
  - TASK-128.4
  - TASK-128.5
  - TASK-128.7
  - TASK-128.6
  - TASK-128.8
  - TASK-128.9
documentation:
  - docs/work-queue-tui-proposal.md
priority: high
type: feature
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
This is the first usable queue. It browses Backlog.md and GitHub Issues together, shows readiness, coverage, freshness, and claim state honestly, and offers both a TUI and a JSON query. It is deliberately read-only: the model has to work across local and remote sources and authorities before any provider write or claim ownership exists. See plan sections 4, 5, 7, 12, 13, and 16 (S2), and decisions D8-D11, D13, D14, D23, and D25.

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every S2 child task is Done
- [x] #2 With one Backlog.md and one GitHub source configured, `worklease queue` and `worklease queue query --json` show the same items, readiness, coverage, freshness, authority, and claim state
- [x] #3 Incomplete dependency graphs, unreadable sources, stale data, and local versus remote authority scope are visible in both the TUI and JSON
- [x] #4 No queue code path reaches a provider write, or a claim acquire, heartbeat, checkpoint, transfer, release, or guarded operation
- [x] #5 With only local sources and the local authority selected, and Backlog.md remote operations off, a queue run makes no network connection. A remote-authority view contacts only the selected authority for coordination and the explicitly configured sources for provider reads
- [x] #6 Claim, MCP, server, and store packages do not import queue or TUI packages (D8)
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reverify all S2 child completion and integrated read-only behavior on main, including TUI/JSON parity and safety. 2. Run repository quality gates and record evidence for each criterion. 3. Commit task finalization in an isolated worktree, merge to main, and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
S2 integration on main (731a8d9): all nine children Done. Go acceptance tests TestCombinedBacklogAndGitHubQueryTUIParity, TestQueueQueryAndTUIFixtureParity, TestQueueStaleSnapshot*, TestQueueRemoteViewUsesSelectedAuthorityWithoutLocalFallback, TestReadOnlyFixtureNetworkAndAuthorityBoundary, TestLocalQueueSandbox, TestQueueHasNoMutationCallPath, and TestQueueImportBoundary passed via mise run test. mise run lint, format-check, typecheck passed. Review: no new code defects; parent is an integration checklist, no source changes required.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Verified all nine S2 children on main: combined Backlog/GitHub TUI and JSON parity, incomplete/stale/offline and authority-scope visibility, read-only call boundary, local network sandbox and remote authority allowlist, and reverse import boundary. All repository quality gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
