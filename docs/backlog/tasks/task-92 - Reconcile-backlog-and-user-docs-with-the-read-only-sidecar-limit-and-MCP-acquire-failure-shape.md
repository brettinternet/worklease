---
id: TASK-92
title: >-
  Reconcile backlog and user docs with the read-only sidecar limit and MCP
  acquire failure shape
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-12 22:39'
updated_date: '2026-09-13 00:21'
labels:
  - go-rewrite
dependencies: []
references:
  - docs/backlog/docs/go-rewrite/doc-3 - Go-Rewrite-Capability-Inventory.md
  - docs/mcp.md
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
- [ ] #1 Every backlog record and doc that claims read-only opens create no files is corrected or annotated to point at the amended contract
- [ ] #2 docs/mcp.md and the MCP instructions describe acquire failure recovery correctly: retry by lease reference only for uncertain outcomes, and a fresh acquire after a definitive failure
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
<!-- SECTION:NOTES:END -->
