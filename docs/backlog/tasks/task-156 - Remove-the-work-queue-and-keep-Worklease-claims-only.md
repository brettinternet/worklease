---
id: TASK-156
title: Remove the work queue and keep Worklease claims-only
status: To Do
assignee: []
created_date: '2026-10-22 01:23'
labels:
  - work-queue
  - breaking
dependencies: []
references:
  - 'https://github.com/brettinternet/worklease/tree/v1.9.3'
  - 'https://github.com/brettinternet/backlog/blob/main/docs/design.md'
  - docs/work-queue-tui-proposal.md
  - internal/cli/run_commands.go
  - internal/queueui
priority: high
type: task
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Why

Worklease's work queue grew into a tracker inside a claim tool: views, provider adapters, readiness, focused writes, recovery receipts, launches, and an external adapter protocol. That conflicts with D1 of `docs/work-queue-tui-proposal.md`, and the owner wants richer task management with hooks and extensions, which Worklease's own register rules out (D17, D26). Task management moves to `bl` (github.com/brettinternet/backlog). `bl` integrates with Worklease only through Worklease's stable `--json` claim CLI, and Worklease never depends on `bl`. Worklease goes back to doing one thing well: claims.

Tag `v1.9.3` keeps the queue code and its provider probe evidence, which `bl` ports from (bl design.md D24, D25). Nothing outside this repository uses `worklease queue`; the owner is the only user.

## Remove

- `worklease queue` and `q`, with every subcommand: init, query, next, claims, recovery, adapter, authority-id, and launch.
- `internal/queue`, `internal/queueindex`, `internal/sampleadapter`, the queue parts of `internal/cli` and `internal/config`, and `cmd/queue-benchmark-memory`, `cmd/worklease-reference-adapter`, and `cmd/worklease-sample-adapter`.
- The MCP `queue_next` tool and the D8 exception that let MCP import queue packages.
- The queue journal check in the run supervisor (`queueRecoveryJournal`, the `queueAcquirePin` naming). A run still keeps its claim for unresolved guarded operations.
- Queue docs: `queue.md`, `queue-benchmark-results.md`, `external-adapter-protocol.md`, `docs/external-adapter/`, `config-schemas/queue.schema.json`, and `work-queue-tui-proposal.md`. Remove the queue sections of README.md, cli-reference.md, the man page, instructions, and doc tests.
- Tooling used only by the queue: the `queue-benchmark` and `beads-check` mise tasks, the `bd` pin, the `TestQueue` race split, queue scripts, and provider fakes such as `testkit.FakeBacklog` once nothing else uses them.
- Queue-only skill references, such as `external-adapter-authoring.md`. Update `SKILL.md`, AGENTS.md (the `queue next` guidance), and the managed instructions.

## Keep

- Every claim command, the remote authority, resource policies (including the `github`, `linear`, and `backlog-md` providers used for claim keys), `worklease key`, `worklease run` and `runs`, MCP claim tools, and the provider-neutral coordination skill.
- The bare-`worklease` Claims TUI (TASK-145, TASK-150), moved out of `internal/queueui` into a claims-only package, with queue tabs and benchmarks removed.
- The JSON schema version and output of every remaining command, byte for byte.

## Migration

Leftover 1.9.x `queue.yaml`, `queue-sources.yaml`, the queue index, and the recovery journal must never make a command fail. `worklease doctor` reports each leftover with its path and says it can be deleted, and the CHANGELOG points queue users to `bl`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease queue`, `worklease q`, and MCP `queue_next` are gone, and `rg -n -i 'queue' --glob '!CHANGELOG.md' --glob '!docs/backlog/**'` finds no remaining queue feature in code, docs, skills, or instructions
- [ ] #2 Every remaining command's `--json` output and exit codes are unchanged, shown by the existing CLI, MCP, and remote acceptance tests passing unmodified, apart from deleted queue tests
- [ ] #3 Bare `worklease` opens the claims-only TUI, which inspects, renews, and releases claims as before, with its tests moved and passing
- [ ] #4 With 1.9.x queue config, index, and journal files present, every command still works, and `worklease doctor` lists each leftover path as safe to delete
- [ ] #5 `go.mod` drops dependencies only the queue used, and `mise run ci` passes with lower race-suite wall time recorded in Implementation Notes
- [ ] #6 The CHANGELOG marks the removal as breaking and points to github.com/brettinternet/backlog
<!-- AC:END -->
