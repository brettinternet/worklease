# Project Agent Instructions

## Backlog is the plan of record

This repository tracks its own work with native `bl` (github.com/brettinternet/backlog), coordinated through Worklease itself (`coordination.worklease` in `backlog.yaml`). Treat `backlog/<ID>.md` and `backlog/archive/<ID>.md` as durable project state. Read and change tasks only through `bl` (CLI or MCP); never edit item Markdown directly.

The CLI is installed through `mise`. Prefer the explicit form below when the command is not already on `PATH`:

```bash
bl <command>
```

Read `bl instructions` and `bl instructions workflow` before acting; use the installed binary's guidance, not commands from a newer design or another tracker.

Do not search for or create a backlog item for work expected to finish in the current session, even if it produces a commit. Questions, explanations, exploration, and mechanical requests also need no item. Use `bl` only when the user requests it or the work needs durable state across sessions.

For work that meets that threshold, inspect existing work before starting:

```bash
bl ls --json
bl ls "<query>" --json
```

Use an existing task when it covers that work; otherwise create one (`bl instructions workflow` covers creation). Do not create duplicate tasks. Query syntax: `bl instructions query`.

## Working on backlog tasks

Pick and claim work in one step, before changing any status. Use one stable session for your whole loop:

```bash
bl next --claim --start --session SESSION --json
```

This is `bl next --claim --start`: one ranked snapshot, skip confirmed contention, stop on an uncertain acquire. A held open item resumes without acquiring; a pending acquire replays its exact retained request. Keep the same session on uncertainty; never bypass recovery with a new session. `bl next` alone is read-only.

Read the item fully before changing anything:

```bash
bl show ID --json
```

Keep status, plan, progress notes, and acceptance checks in the task through `bl note ID TEXT`, `bl ac ID check N --evidence TEXT`, and `bl edit ID --section plan` (or `summary`/`dod`). Renew the claim with `bl heartbeat --session SESSION` at least every few minutes and before half the TTL. Read `show ID --json` first and pass `--if-version VERSION` when acting on criterion indices. Respect dependencies and acceptance criteria; do not silently expand scope. If the approach changes, update the task plan before continuing.

Before completion, record each criterion's command and observed result with `bl ac ID check N --evidence TEXT`. Verify every acceptance criterion with objective evidence, then finish with:

```bash
bl done ID --session SESSION --json
```

`bl done` writes then releases before returning. If stopping incomplete, persist progress then `bl release ID --session SESSION`. A claim, assignee, branch, worktree, or lock is not a substitute for durable task state.

## Quality gates

The pre-commit hook runs gofmt on staged files and `go vet`, `staticcheck`, and `go test` on packages with staged Go files (the full suite when `go.mod` or `go.sum` changes). CI runs everything. Before handing off, run the checks that match what you changed, and fix every reported failure instead of bypassing or weakening a check:

| Change | Run |
|---|---|
| Docs, skills, or Backlog records only | Nothing; `mise run doc-test` if documented commands changed |
| Internals of one package | The hook covers it: `go test ./<pkg>` |
| Exported API, a shared package (`testkit`, `store`, `lease`, `config`), or several packages | `mise run lint` and `mise run test` |
| Concurrency, locking, or process lifecycle | Also `mise run race` |
| Before a PR or final handoff of a multi-package change | `mise run lint`, `mise run format-check`, `mise run typecheck`, and `mise run test` |

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
- Provider discovery and writes belong to callers; claim-key policy tests do not require provider CLIs.
- Write benchmarks as `Benchmark*` functions or gate them behind an environment variable such as `AUTHORITY_SAMPLES`. Never assert absolute latency in the default run.
- Use `cmd/worklease-smoke` and `cmd/worklease-remote-smoke` only for shipped-binary, multi-process behavior that in-process tests cannot reach. Their `_test.go` files test only the harness's own verification logic.
- Before handing off, run new or changed tests with `go test -race -count=3 -run '<TestName>' ./<package>`.

| Tier | Command | Runs in |
|---|---|---|
| Affected package tests | `mise run hooks` | pre-commit hook |
| Package tests | `mise run test` | CI on linux-x64, linux-arm64, macos-x64, and macos-arm64 |
| Race | `mise run race` | CI linux-x64 |
| End-to-end | `mise run e2e` (built-binary smoke, remote smoke, doc test) | CI linux-x64 |
| Authority load probe | `mise run authority-benchmark` | manual |
| VM remote smoke | `mise run remote-smoke-vm` | manual |

## Worklease workflow

For agent coordination that needs claims, dependency-aware selection, heartbeats, durable progress, review boundaries, or archival, read [`docs/worklease-workflow.md`](docs/worklease-workflow.md) and then [`skills/worklease-workflow/SKILL.md`](skills/worklease-workflow/SKILL.md). They define the provider-neutral coordination contract: work sources and item IDs are opaque, and the caller supplies discovery, mutation, resource, and authority capabilities. Do not add provider assumptions to that contract or treat local coordination as provider-side fencing.

Select work through `bl`; use Worklease's claim CLI for bounded ownership when coordinating concurrent workers.

When caller context does not already provide those source/provider capabilities,
read
[`skills/worklease-workflow/references/source-workflow.md`](skills/worklease-workflow/references/source-workflow.md)
after the generic contract. It maps caller-selected Backlog.md, loose Markdown,
GitHub Issues, Linear, Jira, and custom sources into that contract. Load only
the matching provider reference, keep provider credentials and writes
caller-authorized, and leave graph selection and claim lifecycle in
`worklease-workflow`.
