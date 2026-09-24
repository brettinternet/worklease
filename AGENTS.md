# Project Agent Instructions

## Backlog is the plan of record

This repository uses Backlog.md for work that needs durable planning or coordination across sessions. Treat the task, document, decision, and milestone records under `docs/backlog/` as durable project state. Do not edit those Markdown files directly; use the `backlog` CLI so IDs, metadata, relationships, and structured sections remain valid.

The CLI is installed through `mise`. Prefer the explicit form below when the command is not already on `PATH`:

```bash
backlog <command>
```

When work needs cross-session planning, coordination, review, or handoff, read the overview before deciding whether to use an existing task or create one:

```bash
backlog instructions overview
```

Do not search for or create a backlog item for work expected to finish in the current session, even if it produces a commit. Questions, explanations, exploration, and mechanical requests also need no item. Use Backlog.md only when the user requests it or the work needs durable state across sessions.

For work that meets that threshold, inspect existing work before starting:

```bash
backlog search "<terms>" --plain
backlog task list --plain
```

Use an existing task when it covers that work. Otherwise create one through `backlog task create`; do not create duplicate tasks.

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

## Quality gates

Before handing off or committing changes:

- Run `mise run lint`, `mise run format-check`, `mise run test`, and `mise run typecheck`; fix every reported failure instead of bypassing or weakening a check.
- Stage the intended files and run `mise run hooks`; fix formatting and test failures before committing.
- Install the Git hook once with `mise run hooks-install`, and do not disable Lefthook or skip failing jobs to force a commit.

## Testing

Write a test only when it would catch a real regression, and cover each behavior once. These rules apply to new and edited tests. Older tests were written before them; do not mass-rename or rewrite unrelated tests, because TASK-136 tracks that cleanup.

- Put each test in the `_test.go` file for the behavior it covers. Do not name files after where a finding came from (`review_*_test.go`, `*_regression_test.go`).
- Add one regression test per fixed bug, at the lowest layer that reproduces it. Do not repeat the same assertion at the CLI, MCP, and smoke layers.
- Control time with `testkit.Clock` or an injected clock. Never `time.Sleep` to wait out a TTL, timeout, or poll interval; wait for something observable, with a bounded deadline.
- Prefer in-process calls: the CLI through `Run` or `testkit.RunCLI`, and MCP through `mcp.Server`. Do not run `go build` in tests. When you need a separate process, re-execute the test binary with `testkit.RunTestProcess`, which bounds the process group and cleans it up.
- Isolate state with a per-test `testkit.Home(t)` and create Git fixtures with `testkit.GitCommand`. Every package that has tests keeps the `isolation_test.go` `TestMain` that calls `testkit.IsolateProcessEnvironment`.
- Call `t.Parallel()` unless the test changes process-global state (`t.Setenv`, `os.Setenv`, `os.Chdir`, or package-level variables).
- Treat a flaky test as a bug: fix the cause instead of retrying, widening tolerances, or skipping. Skip only when a required external tool is missing, and only after checking that the tool actually runs.
- Write benchmarks as `Benchmark*` functions or gate them behind an environment variable such as `QUEUE_*_SAMPLES`. Never assert absolute latency in the default run.
- Use `cmd/worklease-smoke` and `cmd/worklease-remote-smoke` only for shipped-binary, multi-process behavior that in-process tests cannot reach. Their `_test.go` files test only the harness's own verification logic.
- Before handing off, run new or changed tests with `go test -race -count=3 -run '<TestName>' ./<package>`.

| Tier | Command | Runs in |
|---|---|---|
| Package tests | `mise run test` | pre-commit hook; CI on linux-x64, linux-arm64, macos-x64, and macos-arm64 |
| Race | `mise run race` | CI linux-x64 |
| End-to-end | `mise run e2e` (built-binary smoke, remote smoke, doc test) | CI linux-x64 |
| Relative queue benchmarks | `mise run queue-benchmark` | CI pull requests (base vs. head, report only) |
| Opt-in probes | `QUEUE_TERMINAL_SAMPLES`, `QUEUE_COLD_PAGE_SAMPLES`, or `QUEUE_COMBINED_SAMPLES` with `go test ./internal/queueui`; `mise run authority-benchmark` | manual |
| VM remote smoke | `mise run remote-smoke-vm` | manual |

## Worklease workflow

For agent coordination that needs claims, dependency-aware selection, heartbeats, durable progress, review boundaries, or archival, read the generated guide at `docs/backlog/docs/worklease-workflow/doc-1 - Worklease-Workflow.md` and then [`skills/worklease-workflow/SKILL.md`](skills/worklease-workflow/SKILL.md). They define the provider-neutral coordination contract: work sources and item IDs are opaque, and the caller supplies discovery, mutation, resource, and authority capabilities. Do not add provider assumptions to that contract or treat local coordination as provider-side fencing.

When caller context does not already provide those source/provider capabilities,
read
[`skills/worklease-workflow/references/source-workflow.md`](skills/worklease-workflow/references/source-workflow.md)
after the generic contract. It maps caller-selected Backlog.md, loose Markdown,
GitHub Issues, Linear, Jira, and custom sources into that contract. Load only
the matching provider reference, keep provider credentials and writes
caller-authorized, and leave graph selection and claim lifecycle in
`worklease-workflow`.

The short Backlog.md command nudge in this file is managed by `backlog agents`. Keep the managed block intact; refresh it after changing project instructions with:

```bash
backlog agents --update-instructions
```

The detailed contracts remain in the generated Backlog guide and reusable skills; the generated nudge should point agents there rather than duplicate them.

<!-- BACKLOG.MD GUIDELINES START -->
<!-- backlog.md-instructions-version: 1.48.0 -->
<CRITICAL_INSTRUCTION>

## Backlog.md Workflow

This project uses Backlog.md for task and project management.

Only for work that needs durable planning or coordination across sessions, run `backlog instructions overview` to decide whether to search, read, create, or update Backlog tasks. Do not use Backlog.md for work expected to finish in the current session unless the user requests it.

Before task lifecycle actions, read the matching detailed guide:
- `backlog instructions task-creation` before creating or splitting tasks
- `backlog instructions task-execution` before planning, changing status or assignee, adding a plan or implementation notes, or implementing task work
- `backlog instructions task-finalization` before checking acceptance criteria, writing final summaries, or moving tasks to terminal statuses

Use `backlog <command> --help` before running unfamiliar commands. Help shows options, fields, and examples.

Do not edit Backlog task, draft, document, decision, or milestone markdown files directly. Use the `backlog` CLI so metadata, relationships, and history stay consistent.

</CRITICAL_INSTRUCTION>
<!-- BACKLOG.MD GUIDELINES END -->
