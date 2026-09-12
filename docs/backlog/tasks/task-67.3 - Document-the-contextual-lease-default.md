---
id: TASK-67.3
title: Document the contextual lease default
status: To Do
assignee: []
created_date: '2026-09-12 02:04'
labels:
  - cli
  - docs
dependencies:
  - TASK-67.2
references:
  - README.md
  - docs/cli-reference.md
  - docs/mcp.md
  - src/worklease/instructions.py
  - skills/worklease-workflow/SKILL.md
  - docs/backlog/docs/worklease-workflow/doc-1 - Worklease-Workflow.md
parent_task_id: TASK-67
priority: high
type: docs
ordinal: 76000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Update user-facing and agent-facing documentation for the contextual default delivered by TASK-67.2. Behavior, location, precedence, and naming are settled in the TASK-67 description; describe them, do not redesign them. Every example must be valid against the shipped CLI.

The Backlog guide doc-1 is Backlog-managed: edit it with `backlog doc update doc-1 --content ...`, never by hand. `worklease instructions` text lives in `src/worklease/instructions.py` and is shared by the CLI and the MCP server, so keep it concise and interface-neutral.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The README lifecycle example uses the path-free flow (no `mktemp`, `LEASE_FILE`, or path-based `trap`) and a short section explains the `context-leases/` location, the Git-root, linked-worktree, and non-Git context rules, `-L PATH` for concurrent leases in one context, and `--no-lease-file`.
- [ ] #2 `docs/cli-reference.md` adds `-L` to the short option table, documents the precedence rules and the `lease-context-missing` and `lease-context-conflict` reasons, adds `LEASE_FILE` to the text grammar, and covers the handle directory in the state selection section.
- [ ] #3 `worklease instructions loop` and `instructions safety` describe the contextual default, reserve `--lease-file` for concurrent or automated leases, and their existing tests are updated.
- [ ] #4 `skills/worklease-workflow/SKILL.md` and Backlog guide doc-1 (updated through `backlog doc update`) show the path-free CLI loop and keep the `-L PATH` form for concurrent leases.
- [ ] #5 `docs/mcp.md` states that MCP `mcp-leases/` handles and CLI `context-leases/` handles are distinct and its operator recovery command uses `-L`.
- [ ] #6 No documentation example references removed behavior, and `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
