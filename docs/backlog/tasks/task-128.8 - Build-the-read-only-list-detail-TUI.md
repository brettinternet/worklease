---
id: TASK-128.8
title: Build the read-only list/detail TUI
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 23:56'
labels:
  - work-queue
  - tui
  - reviewed
milestone: m-1
dependencies:
  - TASK-128.1
  - TASK-128.2
  - TASK-128.3
  - TASK-128.7
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: feature
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`worklease queue` is the human surface: a dense list/detail layout, Vim-first keys, and honest labels for every reason work is unavailable. Plan section 13 includes mockups. The TUI renders core snapshots only. Actions that arrive later (claim, assign, progress, launch) appear but are disabled with an explanation, so users can discover them. The Recovery tab arrives with TASK-132.4.

Provider text is untrusted: titles, bodies, and comments can contain terminal escape sequences and attacker-written content (plan sections 10 and 13). Use the library versions pinned by TASK-128.1.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `worklease queue [--view NAME]` shows a header (view, authority profile and ID with local or remote scope, me, sources healthy/total, sync age), a views pane with counts, a sources pane with per-source status, the item list, and a detail pane with Summary, Dependencies, Activity, and Claims tabs
- [x] #2 The keys in the plan section 13 table work for navigation, filtering, open, back, refresh, the command palette, help, and opening the provider URL. Keys for future actions show why the action is unavailable
- [x] #3 Assignment, native claim state, and Worklease claim are separate columns. A claim detail always shows authority and scope. Blocked, occupied, assigned elsewhere, unknown dependencies, stale, permission denied, offline, and rate-limited states are each labeled distinctly and never shown as "no work"
- [x] #4 The coverage line shows loaded versus total (exact, estimated, or unknown) and edge coverage. Search states whether it covers loaded rows or the whole source
- [x] #5 The Dependencies tab shows each relationship's type, required outcome, observed evidence, coverage and freshness, and the exact reason the item is ready, blocked, or unknown
- [x] #6 Session labels may be shortened for display, but JSON and stored identities always use full session IDs
- [x] #7 The selection stays anchored by canonical identity across refreshes, and late detail responses never steal focus or reorder the list
- [x] #8 Below 100 columns the TUI switches between list and detail instead of squeezing columns. NO_COLOR and high-contrast modes, resize, and Unicode width work, and state is conveyed with text labels, not color alone
- [x] #9 Provider text is stripped of control and OSC sequences and rendered as bounded, sanitized text. Opening a URL requires an explicit key and uses safe argument passing. The TUI never opens raw Backlog.md files
- [x] #10 Model and update tests drive the TUI with scripted key messages and snapshot inputs, with no real terminal. A render test proves that injected escape sequences are stripped
- [x] #11 The Claims tab shows the current holder's full agentId and claim sessionId, and loads the item's retained claim epochs on demand through the existing authority History call (one resource per request, paged). Each epoch lists agentId, sessionId, acquired and ended times, and end reason, so an item's past worker sessions can be looked up
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Build the read-only Bubble Tea list/detail model over queue snapshots and authority history.
2. Wire the queue command, safe URL action and refresh/detail loading.
3. Add scripted rendering/navigation/security tests, run quality gates, integrate and record evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented initial read-only Bubble Tea queue in isolated task-128.8-tui worktree: snapshot list/detail, keyboard navigation/filter, source and claim labels, lazy paged History, sanitized provider rendering, safe explicit URL action, scripted model tests. Focused Go tests pass; finishing quality gates and integration.

Merged to main in 51793a1, 8163047, 966948c. Scripted Bubble Tea model tests exercise navigation, view/header/coverage, resize/narrow switching, Unicode text, NO_COLOR/escape sanitization, disabled actions, dependency evidence, selection anchoring, lazy paged claim history and full IDs. CLI tests cover JSON refusal and source error labels. All required gates passed in final tree: mise run lint, format-check, test, typecheck, hooks; govulncheck found no vulnerabilities. One general review pass found a stale-offset panic; fixed and regression-tested. Follow-up concrete coverage footer truncation fixed and regression-tested. No outstanding item blocker; private queue.yaml is absent, so no live provider terminal session was claimed as verification.

Review: shared claim-filter predicate (held/free) with JSON; per-source 'me' identity; history cleared on selection change; backward epoch paging on 'm' (n/N stay navigation, comments also page on 'm'); guarded duplicate page requests; widths at 80/100 cols; refresh reports completion/failure only after it finishes (c0260cd, a6991f2). Merged to main 013a058.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added a read-only queue list/detail TUI with safe provider rendering, source/claim visibility and lazy claim history. Scripted UI/CLI tests and required quality gates pass; merged to main in 51793a1, 8163047, 966948c.
<!-- SECTION:FINAL_SUMMARY:END -->
