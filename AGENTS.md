# Project Agent Instructions

## Backlog is the plan of record

This repository uses Backlog.md for committed, non-trivial work. Treat the task, document, decision, and milestone records under `docs/backlog/` as durable project state. Do not edit those Markdown files directly; use the `backlog` CLI so IDs, metadata, relationships, and structured sections remain valid.

The CLI is installed through `mise`. Prefer the explicit form below when the command is not already on `PATH`:

```bash
backlog <command>
```

Before deciding how to handle a request, read the overview and inspect existing work:

```bash
backlog instructions overview
backlog search "<terms>" --plain
backlog task list --plain
```

Use an existing task when it covers the request. If no task covers planned work, create one through `backlog task create`; do not create duplicate tasks. Questions, explanations, and obvious mechanical edits do not need a new task.

## Working on backlog tasks

For an existing task, read it before changing anything and load the matching guide:

```bash
backlog task view TASK-123 --plain
backlog instructions task-execution
```

Keep status, assignee, plan, progress notes, comments, acceptance checks, and final summary in the task through `backlog task edit`. Re-read command help before using unfamiliar fields. Respect dependencies and acceptance criteria; do not silently expand scope. If the approach changes, update the task plan before continuing.

Before completion, read and follow the finalization guide:

```bash
backlog instructions task-finalization
```

Verify every acceptance criterion with objective evidence, record the evidence in the task, and move the task to its terminal status only after verification. A claim, assignee, branch, worktree, or lock is not a substitute for durable task state.

## Worklease workflow

For agent coordination that needs claims, dependency-aware selection, heartbeats, durable progress, review boundaries, or archival, read the generated guide at `docs/backlog/docs/worklease-workflow/doc-1 - Worklease-Workflow.md` and then [`skills/worklease-workflow/SKILL.md`](skills/worklease-workflow/SKILL.md). They define a provider-neutral contract only: work sources and item IDs are opaque, and the caller supplies discovery, mutation, and authority capabilities. Do not add provider assumptions to the contract or treat local coordination as provider-side fencing.

The short Backlog.md command nudge in this file is managed by `backlog agents`. Keep the managed block intact; refresh it after changing project instructions with:

```bash
backlog agents --update-instructions
```

The detailed workflow remains in the generated Backlog guide and the reusable skill; the generated nudge should point agents there rather than duplicate either document.

<!-- BACKLOG.MD GUIDELINES START -->
<!-- backlog.md-instructions-version: 1.48.0 -->

<CRITICAL_INSTRUCTION>

## Backlog.md Workflow

This project uses Backlog.md for task and project management.

**For every user request in this project, run `backlog instructions overview` before answering or taking action.**

Use the overview to decide whether to search, read, create, or update Backlog tasks.

Before task lifecycle actions, read the matching detailed guide:

- `backlog instructions task-creation` before creating or splitting tasks
- `backlog instructions task-execution` before planning, changing status or assignee, adding a plan or implementation notes, or implementing task work
- `backlog instructions task-finalization` before checking acceptance criteria, writing final summaries, or moving tasks to terminal statuses

Use `backlog <command> --help` before running unfamiliar commands. Help shows options, fields, and examples.

Do not edit Backlog task, draft, document, decision, or milestone markdown files directly. Use the `backlog` CLI so metadata, relationships, and history stay consistent.

</CRITICAL_INSTRUCTION>

<!-- BACKLOG.MD GUIDELINES END -->
