---
id: TASK-153.4
title: Load the committed source contract for enrolled checkouts
status: Done
assignee:
  - '@pi'
created_date: '2026-09-29 02:25'
updated_date: '2026-09-29 10:05'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-153.1
  - TASK-153.2
references:
  - internal/cli/queue_init.go
  - internal/config/queue.go
  - internal/config/queue_identity.go
documentation:
  - docs/queue.md
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-153
priority: medium
type: feature
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-151 lets a repository commit `.config/worklease/queue-sources.yaml`, but only `queue init` reads it, and adoption copies the sources into each user's owner-private `queue.yaml` with no link back. That is safe for a single user and wrong for a team: the claim domain is the one value every collaborator must share, and when the committed domain changes, each user who does not re-run init keeps claiming under the old domain. Their claims silently stop contending with teammates', and no gate fires because their private configuration did not change.

Load the contract at runtime instead, while keeping the D10 trust boundary. The file stays limited to the TASK-151 allowlist (IDs, adapter, workflow, claims), so it never supplies checkouts, credentials, executables, launch argv, network consent, or authority. Enrollment in owner-private configuration is the user's trust decision; an unenrolled checkout is never read. Read the file from the enrolled checkout, not from the current linked worktree, so branches checked out in different worktrees cannot disagree on the claim domain.

This revises D10 ("v1 reads no repository-provided queue configuration") and TASK-151 AC #2 ("no command other than queue init reads the file"). A gitignored in-repo override was considered and rejected: any process in the worktree can write it, and it does not follow into new worktrees.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 For a project the user enrolled in owner-private configuration, the TUI, `queue query`, `queue next`, and MCP `queue_next` read `.config/worklease/queue-sources.yaml` from the enrolled checkout (not the current linked worktree) and apply its source IDs, adapters, workflows, and claim domains at runtime; an unenrolled checkout's file is never read.
- [x] #2 The TASK-151 allowlist and bounds are unchanged, and every privileged key is still rejected by name.
- [x] #3 A contract change to any claim input disables claim actions for the affected source until `queue identity confirm`, while reads continue; no path lets a user keep claiming under a superseded claim domain.
- [x] #4 Owner-private configuration cannot override the contract's claim domain; any contract field it may override is documented with its precedence.
- [x] #5 A missing or malformed contract marks only that project's sources incomplete with a named diagnostic; other projects are unaffected.
- [x] #6 Sources adopted by copy under TASK-151 keep working, and a documented, previewed step converts them to contract-backed sources.
- [x] #7 docs/work-queue-tui-proposal.md records the revised D10, including the rejected gitignored override and why; docs/queue.md and CHANGELOG.md are updated.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a validated owner-private contract enrollment reference and an explicit queue-init opt-in that previews the YAML change; keep legacy copied sources unenrolled by default.
2. Reuse the existing TASK-151 strict parser to load contracts only from enrolled canonical checkout roots, merge contract-owned ID/adapter/workflow/claims over owner-private runtime fields, and return named source-local diagnostics without widening the allowlist.
3. Apply effective contract configuration consistently to TUI, query, next/MCP, and identity confirmation; block claims on missing/malformed contracts and revalidate enrolled contracts immediately before claims.
4. Add focused tests for linked roots, enrollment isolation, precedence, drift, diagnostics, and migration preview; update the queue schema, docs, and Unreleased changelog, then run focused race tests and repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented explicit owner-private contractCheckout enrollment via queue init --enroll-contract (dry-run previews; existing copies stay independent). Runtime loader reuses the unchanged TASK-151 parser and canonical enrolled-main-checkout resolution; contract ID/adapter/workflow/claims take precedence, with source-local missing/invalid/conflict diagnostics. Query, next/MCP, TUI, Start work, writes and identity confirmation use effective contracts; stale claim paths revalidate before acquiring.
Verification: new TestQueueContract* and TestQueueInitContract* tests passed with go test -race -count=3 in internal/cli and internal/config. Coverage includes linked-main contract selection, unenrolled malformed-file non-reading, copy precedence, domain drift blocking claims while reads work, successful reconfirmation, missing/malformed project isolation and named privileged-key rejection, conflicting renames, and dry-run migration with renamed-source confirmation guidance. Existing strict proposal/parser, CLI/TUI and MCP suites passed.
Quality gates passed: mise run lint, format-check, typecheck, test, race, and doc-test. Full race suite passed before final enrollment guidance wording/renamed-source command correction; affected contract tests then reran under race count=3, and all non-race gates reran. An earlier broad focused race command exceeded the tool timeout; the subsequent complete race suite passed without skipped failures.
Implementation child timed out before validation; parent took over and completed one bounded item-scoped review. Fixed enrollment outcome being overwritten, stale Start-work private-config loading, cross-project TUI diagnostic spillover, colliding rename fallback, and renamed-source confirmation guidance. No remaining known task-scoped defects or external blockers. Next step: commit verified implementation, record commit and mark done.

Committed implementation on main as 8830342 (Load enrolled queue source contracts at runtime). Staged-file hooks and commit hook passed. All seven acceptance criteria verified by the focused contract tests, existing parser/CLI/MCP suites, documentation test and recorded review. No remaining blocker or resumable implementation step; no push performed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Runtime queue source contracts now require explicit owner-private enrollment, use the enrolled main checkout across linked worktrees, override shared source fields, and fail closed on claim-input drift or missing/invalid contracts without disabling other projects. Existing copies remain compatible; migration is previewed with --enroll-contract --dry-run. Implemented in 8830342 on main. Verified with focused race tests (count=3), lint, format-check, typecheck, full tests, full race suite, doc-test and pre-commit hooks. No remaining blockers.
<!-- SECTION:FINAL_SUMMARY:END -->
