---
id: TASK-92
title: >-
  Reconcile backlog and user docs with the read-only sidecar limit and MCP
  acquire failure shape
status: Done
assignee:
  - '@brett'
created_date: '2026-09-12 22:39'
updated_date: '2026-09-13 00:24'
labels:
  - go-rewrite
dependencies: []
references:
  - docs/backlog/docs/go-rewrite/doc-3 - Go-Rewrite-Capability-Inventory.md
  - docs/mcp.md
modified_files:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - docs/backlog/docs/go-rewrite/doc-3 - Go-Rewrite-Capability-Inventory.md
  - docs/backlog/tasks/task-69 - Add-a-global-paginated-history-event-feed.md
  - docs/backlog/tasks/task-83 - Add-Worklease-environment-diagnostics.md
  - docs/backlog/tasks/task-85 - Rewrite-Worklease-in-Go.md
  - docs/backlog/tasks/task-85.4 - Select-and-prove-the-SQLite-implementation.md
  - >-
    docs/backlog/tasks/task-85.6 -
    Implement-secure-authority-storage-and-POSIX-locking.md
  - >-
    docs/backlog/tasks/task-85.10 -
    Implement-private-contextual-lease-handles-and-credentials.md
  - docs/mcp.md
  - internal/instructions/instructions.go
  - internal/mcp/mcp.go
  - internal/mcp/mcp_test.go
priority: low
type: docs
ordinal: 117000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-88 amended contract sections 8 and 13 to record that SQLite recreates private -wal/-shm sidecars for read-only opens and that modernc cannot refuse it. The TASK-85.4 comment on TASK-85 and possibly doc-3 (capability inventory) still claim read-only observation without filesystem creation. TASK-88 also changed MCP acquire so a definitive failure removes the pending grant and no longer returns a lease reference in the error details; docs/mcp.md and the MCP tool descriptions need to be checked so clients are not told to retry a failed acquire by reference.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every backlog record and doc that claims read-only opens create no files is corrected or annotated to point at the amended contract
- [x] #2 docs/mcp.md and the MCP instructions describe acquire failure recovery correctly: retry by lease reference only for uncertain outcomes, and a fresh acquire after a definitive failure
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reconcile every historical Backlog record and capability document with the amended SQLite sidecar limitation using supported Backlog CLI mutations.
2. Correct MCP user documentation, server instructions, and acquire tool metadata for uncertain versus definitive acquire failures.
3. Add focused tests, run repository quality gates, review the diff, and integrate the committed work into main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed with the system-installed Worklease CLI as agent C3 using the canonical Backlog.md TASK-92 resource. Audited stale read-only wording and annotated TASK-69, TASK-83, TASK-85, TASK-85.4, TASK-85.6, and TASK-85.10 through supported Backlog CLI writes; updated doc-2 and doc-3 through backlog doc update.

Focused go test ./internal/mcp ./internal/instructions and the required mise run lint, format-check, test, and typecheck gates passed in the implementation worktree. LSP diagnostics report no issues in the three changed Go files; git diff --check passed.

Objective acceptance verification validated 18 required correction passages across 11 authoritative files and rejected the stale contract phrase. Focused MCP instruction/acquire tests passed. Post-merge mise run test passed on main. Implementation commit: a576421; integration merge: 33ea05c.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Reconciled stale read-only SQLite claims with the documented private WAL/SHM sidecar limit across the product contract, capability inventory, and affected Backlog records. Clarified user, canonical-agent, MCP server, and acquire-tool guidance so only uncertain outcomes with a returned lease reference are replayed; definitive failures require a fresh acquire. Verified with 18 explicit documentation assertions, focused MCP tests, all required repository gates, staged hooks, and a post-merge full test run.
<!-- SECTION:FINAL_SUMMARY:END -->
