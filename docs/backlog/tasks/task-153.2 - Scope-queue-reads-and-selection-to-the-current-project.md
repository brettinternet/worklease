---
id: TASK-153.2
title: Scope queue reads and selection to the current project
status: Done
assignee: []
created_date: '2026-09-29 02:25'
updated_date: '2026-09-29 05:47'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-153.1
references:
  - internal/cli/queue_command.go
  - internal/cli/queue_init.go
  - internal/mcp/mcp.go
  - internal/queueui/model.go
documentation:
  - docs/queue.md
  - docs/queue-tui.md
parent_task_id: TASK-153
priority: high
type: feature
ordinal: 95000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Views are the only way to segment the queue, and a view bundles a source set with a filter and an authority. `queue init` appends each newly configured checkout's source to the shared default views, the TUI opens on `Views[0]` whatever the working directory, and neither `queue next` nor MCP `queue_next` has any notion of the current project. With several projects configured, the documented agent loop (`queue next --view Ready --claim`) run in repo A can claim and start repo B's item, and the only workaround is one view per project per filter.

Sources already carry what is needed to match a checkout: Backlog.md and Beads sources have `checkout`, and GitHub sources have `host`/`repository`, which can be matched against the checkout's `origin` (as `queue init` already does). Linear and external sources have no checkout link today.

Resolve the project from any linked worktree using the repository resolution from the worktree-bindings subtask, so an agent in `.worktrees/<name>` gets its own project's scope.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 From a configured project's main checkout or any of its linked worktrees, the TUI, `queue query`, and `queue next` (with and without `--claim`) default to that project's sources: Backlog.md and Beads sources whose checkout is that repository, and GitHub sources matching its `origin`.
- [x] #2 Cross-project selection requires explicit opt-in through a CLI flag and a TUI toggle; outside any configured project the default is all sources; human and JSON output name the effective scope.
- [x] #3 MCP `queue_next` applies the same default scope for the checkout the server was started in, and cross-project selection requires an explicit argument.
- [x] #4 Linear, external, and other sources without a checkout can be associated with a project in owner-private configuration; unassociated sources appear only in the cross-project scope.
- [x] #5 Views filter within the effective scope, so one set of Ready/Mine/Claimed/All views serves every project and `queue init` in a new project needs no view edits; existing `queue.yaml` files load unchanged, and a view that lists sources still restricts to them.
- [x] #6 A regression test shows `queue next --view Ready --claim` run from checkout A never selects or claims checkout B's item when both sources are in the view.
- [x] #7 docs/queue.md, docs/queue-tui.md, docs/mcp.md, docs/cli-reference.md, and skills/worklease-workflow describe project scope; CHANGELOG.md Unreleased has an entry.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add owner-private checkout association and project scope resolution using handle.BindingRoots plus the existing GitHub origin parser; treat unassociated sources as cross-project only, and preserve listed-view source restrictions as an intersection.
2. Apply the same effective source set to query/next, including explicit item selectors and claims; report project/all scope in human and JSON output and add a CLI cross-project opt-in.
3. Pin MCP queue_next to the server startup checkout and require an explicit all-projects argument to widen scope.
4. Make the TUI start in the current project scope and add a cross-project toggle that refreshes into the wider source set without permitting out-of-scope actions.
5. Change queue init defaults to shared filter-only views while loading legacy source-limited views unchanged; document scope behavior and add regression coverage.
6. Run focused tests (including changed tests under race, count=3), then lint, format-check, typecheck, full tests, and race as required.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation is in the main working tree. Affected package tests and full lint, format-check, typecheck, provider-enabled tests passed. Parent review found two concrete TUI defects: recursive scope restart retained backends until final exit, and an empty legacy-view/project intersection could display all loaded project rows. Fixed with frame-scoped cleanup and explicit empty-view filtering; added empty-intersection regression coverage. Full mise run race passed. The initial claim expired during the 18-minute race run (read-only validation); acquired a fresh ownership epoch before any further writes. Remaining: focused repeated race checks, documentation validation, interactive scope-toggle exercise, final commit and provider completion.

Acceptance evidence:
- AC 1/4: TestQueueProjectSourcesMatchLinkedCheckoutOriginAndPrivateAssociations verifies linked-main checkout identity, Backlog/Beads, GitHub origin case matching, Linear/external projectCheckout, and exclusion of unassociated sources. CLI query/claim uses the same resolver; TestQueueNextClaimNeverEscapesCurrentProjectScope exercises project A versus B.
- AC 2: TestQueueProjectSourcesDefaultToAllOutsideConfiguredProject, JSON/text query coverage, explicit --all-projects selection, and real PTY interaction with bin/worklease queue --view All verify named project scope, X to all projects, X back to project, and clean exit.
- AC 3: TestMCPQueueNextPinsStartupProjectScope changes process cwd after server creation, proves A stays selected, then explicitly widens with allProjects.
- AC 5: config filter-only parsing, init merge/concurrent/proposal tests preserve legacy restrictions and avoid source-list edits; TestEmptyProjectViewIntersectionShowsNoRows proves empty intersections cannot show another view’s loaded rows.
- AC 6: claim regression makes B the cross-project winner, then proves default --claim selects only A and an explicit B selector is rejected.
- AC 7: all requested docs, schema, skill, and Unreleased changelog updated; mise run doc-test passed.
Validation: mise run lint, format-check, typecheck, test, race, and doc-test passed. New/changed focused tests passed go test -race -count=3. LSP diagnostics clean for queue_command.go; git diff --check passed. One parent review pass completed after implementation worker timed out; the planned independent reviewer did not launch. Two concrete TUI defects were fixed and affected checks rerun. No remaining blocker; next step is commit and terminal provider checkpoint.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented project-scoped queue reads and selection in cff559efb7f737cde1abc40fca7a478419d624d6 on main. CLI and MCP require explicit cross-project opt-in; the TUI has an X scope toggle. Linked worktrees share their project identity, owner-private projectCheckout associates non-checkout sources, and filter-only views coexist with preserved legacy restrictions. All seven acceptance criteria verified. Full lint, format-check, typecheck, provider-enabled test, race, doc-test, repeated focused race checks, and real PTY toggle exercise passed. Parent review fixed scope-restart backend retention and empty-view filtering. Pre-commit hooks passed. No blocker or remaining step for this item; TASK-153.3 and TASK-153.4 remain separate follow-up work. No push performed.
<!-- SECTION:FINAL_SUMMARY:END -->
