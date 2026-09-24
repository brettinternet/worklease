---
id: TASK-129.3
title: Add incremental GitHub sync and reconciliation
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 21:25'
labels:
  - work-queue
  - github
  - reviewed
milestone: m-1
dependencies:
  - TASK-129.1
  - TASK-129.2
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-129
priority: high
type: feature
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-128.5 enumerates a whole repository on every refresh. Plan section 14 ("GitHub loading") replaces that with incremental sync into the TASK-129.1 index, and GitHub's own guidance makes it subtle. Updated-order pages move records between pages. A 304 on the newest issue proves nothing about older issues, edges, or permissions. A 404 can mean lost permission rather than deletion. The design: `since` filtering with an overlap window and a fixed scan-start watermark; each page persisted atomically with its resume cursor; the committed watermark advanced only after the whole window is scanned; the conditional GET used only as an optional hint; periodic bounded reconciliation for membership and visibility.

Use the TASK-126.5 results. If dependency edits do not change updatedAt, edge freshness relies on reconciliation and on refreshing the closure before any action, and the view must label edges accordingly.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Incremental sync uses the since filter with an overlap window and a fixed scan-start watermark, deduplicates by node ID, and persists each page with its resume cursor in one index transaction. The committed watermark advances only after the complete window is scanned, so a crash or partial newest-first page never skips older changes
- [x] #2 A conditional GET is keyed to its exact origin, principal, query, page, and representation. A 304 is only a hint and never suppresses scheduled incremental sync or reconciliation, as tested by changing an older issue while the newest page returns 304
- [x] #3 Reconciliation runs as bounded, resumable scan generations. A completed generation may retire rows from the accessible projection but never records deletion. Absent items are classified as deleted, moved, inaccessible, or unknown only as the evidence allows, stale content is withheld, and identity and recovery records are kept. No partial scan retires rows
- [x] #4 Visible rows are hydrated through batched `nodes(ids:)` queries sized by the TASK-126.5 limit, and comments load lazily
- [x] #5 Edge freshness is labeled according to the TASK-126.5 finding on whether dependency edits change updatedAt
- [x] #6 Tests against the fake GitHub cover interrupted multi-page sync, an issue edited between pages, overlap deduplication, a newest-page 304 with older changes, permission loss returning 404 for an existing issue, transfers, and expired cursors
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend the queue index for crash-safe GitHub sync and resumable reconciliation. 2. Integrate the incremental adapter on current main; use newest-first updated ordering plus a fixed watermark and overlap to revisit edits moving ahead of the cursor, and recover promptly after lost access. 3. Finish conditional GET hints, evidence-aware absence classification, lazy comments, fake GitHub regressions, project gates, review, and integration.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Partial checkpoint: branch task-129.3-github-sync commit 6f6d7b8 at /Users/brett/dev/me/worklease/.worktrees/task-129.3-github-sync. Implemented page and cursor transactions, bounded reconciliation, incremental GraphQL listing, batch-node primitives, and 404 withholding. Lint, format-check, test, typecheck, hooks pass on this branch. One review found actionable defects: moving updated-order pages can skip changes; restored access delays unchanged issue recovery; OnDemandDetails suppresses batch hydration. Conditional GET/304, lazy comments, absence classification, edge freshness and transfer tests remain. Main advanced concurrently, so reconcile with current main while preserving its changes before integration. Next resume: add failing tests for page movement, access restoration and loader hydration; fix; implement remaining acceptance criteria; rerun gates. Partial branch is not merged.

Resume checkpoint: Worktrunk created branch task-129-3-resume from main at /Users/brett/dev/me/worklease/.worktrees/task-129-3-resume (creation receipt action=created, branch=task-129-3-resume, path as above, base_branch=main; loop session 1bc1a93f-7039-4731-955d-3aa549217aa8). Cherry-picked earlier 6f6d7b8 as 7b17b4c and committed integration fixes as 1990ecf. Resolved concurrent main merge conflicts, fixed scan-cursor eviction, changed incremental updated order to DESC for moving-page overlap, restarted reconciliation after lost source access, enabled batch hydration despite OnDemandDetails, labeled GitHub edge freshness caveat, and updated affected fixture tests. Verification on this branch: mise run lint, format-check, test, typecheck, hooks, hooks-install and Git commit pre-commit hook passed. One targeted review of earlier attempt guided those fixes; no new general review yet. NOT COMPLETE and not merged. Remaining: conditional GET/304 keyed hint and older-issue regression; true absence classification/recovery, lazy comments, transfer/expired-cursor and cross-page dedup regression, verify page movement through loader/index under crash, review and gates, then merge main and clean up only verified owned worktree. Preserve unrelated main modifications, including task-135.

Checkpoint 2026-09-23: commits 5796d08 and 7c345a5 on task-129-3-resume (/Users/brett/dev/me/worklease/.worktrees/task-129-3-resume). Made reconciliation watermark monotonic when incremental sync completes meanwhile; added expired-cursor recovery and transfer hydration regressions, batch hydration now matches immutable node ID rather than issue number. Added optional REST newest-page conditional GET scoped by URL, account, Accept, credential generation; 304 never skips GraphQL incremental/reconciliation. Focused tests and mise run lint, format-check, test, typecheck, hooks plus commit hooks all passed. No general review this pass. NOT COMPLETE, not merged: next resume should prove the hint runs through Loader with an older issue edited behind a 304, add evidence-aware absence classification/recovery and explicit visible-only lazy-comment hydration, cover crash/page movement with real index and fake GitHub, then one proportional review, full gates, finalize and integrate. Existing partial worktree is session-owned; preserve unrelated main modifications. No external blocker.

Checkpoint: e572597 on task-129-3-resume adds Loader-level fake GitHub regression proving a newest-page REST 304 cannot hide an older issue change from incremental sync. Focused queue/index tests and mise lint, format-check, test, typecheck, hooks and commit hook passed. NOT COMPLETE or merged: next implement evidence-aware absence classification/recovery and explicitly lazy visible-only comments; test crash/page movement with actual index + fake GitHub, then one proportional review, full gates, merge main and clean up verified worktree. Preserve unrelated main changes.

Checkpoint: 0f2ab8b on task-129-3-resume withholds private GitHub projection and schedules reconciliation on SAML SSO expiry (403), covered by focused regression. All project gates including hooks and commit hook passed. NOT COMPLETE or merged: continue with evidence-aware absence classification and crash/page-movement integration, one review, final gates, merge and verified worktree cleanup. Unrelated main changes untouched.

Checkpoint: b264f02 on task-129-3-resume removes stale in-memory GitHub refs when the same immutable node appears under a new issue number in incremental pages; regression TestLoaderIncrementalRenumberingRemovesOldReference. Focused test and mise run lint, format-check, test, typecheck, hooks and commit hook passed. NOT COMPLETE or merged: evidence-aware reconciliation absence classification, explicit lazy comments, crash/page movement integration with real index and fake GitHub remain. Continue in existing session-owned worktree; then proportional review, final gates, merge and verified cleanup. Unrelated primary changes untouched.

Checkpoint: task-129-3-resume commits a2d6fc9, a29953b, 9c6e7c4, 343997c. Fake GitHub + real index test exercises interrupted moving-page reconciliation, cursor recovery, atomic node replacement. Persist unknown/moved absence evidence without claiming deletion, batch visible GitHub detail hydration on initial paint and selection, explicit paginated lazy comments. All gates passed after each commit: mise run lint, format-check, test, typecheck, hooks and commit hook. One proportional general review pending; branch not integrated. Main advanced concurrently with TASK-129.5/TASK-130.1; merge main into branch, run gates, integrate without touching unrelated dirty main backlog records. Remaining: act on concrete review findings, verify acceptance criterion evidence and finalize only if complete.

Final evidence: main fast-forward integrated 1e568e3 (branch includes 7b17b4c..c2d8aef). AC1 TestGitHubSyncPageAndWatermarkAreAtomic, TestGitHubIncrementalPagesKeepFixedWatermarkAndOverlap, TestGitHubSyncResumesAfterReopenWithMovedNode; AC2 TestLoaderNewest304StillRefreshesOlderIssue; AC3 TestGitHubReconciliationRetiresOnlyAtCompletedGeneration, TestGitHubWithheldItemRetainsUnknownIdentity, TestGitHubConfirmedAccessLossClassifiesInaccessible, TestGitHubSyncDeduplicatesByNodeIDAcrossReferences; AC4 TestLoaderDefersGitHubDetailsAndBatchesVisibleRows, TestGitHubCommentsLoadOnlyOnExplicitRead, TestActivityLoadsCommentsOnDemandAndIgnoresLatePages; AC5 github.blockedBy interpretation and section 14 document the observed removal without updatedAt; AC6 TestGitHubReconciliationContinuesAcrossFreshAdapters, TestGitHubReconciliationResumesAfterInterruptedMovingPage, TestGitHubIncrementalRevisitsIssueMovedAheadOfCursor, TestGitHub404WithholdsExistingIssueInsteadOfDeletingIt, TestGitHubTransferredNodeWithholdsItemAndDisablesClaims, TestGitHubListRejectsExpiredCursor. One general reviewer pass found four item-scoped defects; each corrected, focused regressions added; no second general pass. mise run lint, format-check, test, typecheck, hooks and commit hook passed after final corrections and main integration. docs/work-queue-tui-proposal.md section 14 updated in c2d8aef. No push. Preserve unrelated primary changes.

Post-completion review: fixed resumed-reconciliation coverage, SAML/401 detail withholding, and in-flight hydration after withhold (3325314).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented crash-safe GitHub incremental sync and bounded reconciliation with evidence-aware absence, live visible hydration and lazy comments. Verified fake GitHub/index regressions, full project gates, one review with fixes, and fast-forward merge to main at 1e568e3.
<!-- SECTION:FINAL_SUMMARY:END -->
