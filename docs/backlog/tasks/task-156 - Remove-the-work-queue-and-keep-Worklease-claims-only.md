---
id: TASK-156
title: Remove the work queue and keep Worklease claims-only
status: Done
assignee: []
created_date: '2026-10-22 01:23'
updated_date: '2026-10-22 07:12'
labels:
  - work-queue
  - breaking
  - reviewed
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
- [x] #1 `worklease queue`, `worklease q`, and MCP `queue_next` are gone, and `rg -n -i 'queue' --glob '!CHANGELOG.md' --glob '!docs/backlog/**'` finds no remaining queue feature in code, docs, skills, or instructions
- [x] #2 Every remaining command's `--json` output and exit codes are unchanged, shown by the existing CLI, MCP, and remote acceptance tests passing unmodified, apart from deleted queue tests
- [x] #3 Bare `worklease` opens the claims-only TUI, which inspects, renews, and releases claims as before, with its tests moved and passing
- [x] #4 With 1.9.x queue config, index, and journal files present, every command still works, and `worklease doctor` lists each leftover path as safe to delete
- [x] #5 `go.mod` drops dependencies only the queue used, and `mise run ci` passes with lower race-suite wall time recorded in Implementation Notes
- [x] #6 The CHANGELOG marks the removal as breaking and points to github.com/brettinternet/backlog
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Remove queue commands, adapters, MCP tool, configuration, and supervisor coupling while preserving claim APIs.
2. Extract the existing Claims TUI into a claims-only package and add legacy artifact doctor diagnostics.
3. Remove queue-only tooling and documentation and update migration guidance and instructions.
4. Diagnose and fix the user-approved intermittent MCP renewal startup failure without changing or weakening the existing tests.
5. Run focused repeated race checks and full CI, perform one direct review, commit and merge to main, clean the owned worktree/workspace and scratch snapshots, then finalize acceptance evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation extracted internal/claimsui and preserved existing claim mutation/authorization tests; removed the queue surface, adapters/index, tooling, docs, and fsnotify dependency. Focused claims/migration/instructions tests passed go test -race -count=3; lint, vet, and doc-test passed. Direct single review found and fixed a lost mouse press-only guard during extraction; regression test added. Full CI and same-host before/after race measurement are next. Existing profile binding tests fail unchanged in linked worktrees (they read per-checkout bindings although production binds the primary root); validating standalone snapshots without modifying those tests. Owned worktree receipt: .git/worktrees/task-156-claims-only/agent-creation.json; branch task-156-claims-only; session 01a16187-d051-7288-ae92-fa488310e717; loop c1e7ab2c-e80b-4cb7-b494-91c34b6a286f.

Source implementation 62a1dbc committed with installed hooks in a byte-identical standalone validation snapshot, then fast-forward merged into main. Same-host original mise race passed in 1117.53 s; claims-only fresh go test -race -count=1 -p 1 -parallel 2 ./... passed in 129.26 s (88.4% lower). Full mise run ci passed, including local/remote smoke, docs, vuln, manual. Later normal full suites exposed TestAutomaticHeartbeatHasOneOwnerAndDoesNotResumeAfterRestart twice (revision stayed 1); 20 focused repetitions, 5 uninstrumented package repetitions, instrumented repeated packages, and full race runs passed. User explicitly approved diagnosing/fixing this pre-existing failure as task scope before cleanup. No tests skipped or weakened. Standalone snapshot is /private/var/folders/jp/1mwx72h172955139pth4h8800000gn/T/worklease-task-156-3v7g8jzc; temporary diagnostics exist only in its after/internal/mcp/renew.go and will be discarded.

Heartbeat root cause established with temporary in-memory diagnostics: initial read-only store open sometimes sees the concurrently unlinked worklease.db-shm sidecar with stat.Nlink == 0 and misclassifies it as a hard link (home-unsafe). A rapid real MCP acquisition/startup diagnostic reproduced at iteration 46; with the fix 1000 iterations passed. Fix at the common store safety boundary translates zero links to ENOENT; >1 links, wrong owner/mode, symlinks and database identity checks remain unchanged. The existing unchanged automatic-heartbeat regression already reproduces this bug; no redundant probabilistic store-loop test or production diagnostic code is retained. Scoped fix is five lines in internal/store/driver.go.

Final verification and delivery:
- AC1: built queue and q invocations return unknown-command JSON and exit 64; unchanged MCP exact-eleven-tool schema test passes. Repository search has only legacy-path diagnostics and generic request/concurrency uses, not a queue feature.
- AC2: full CLI/MCP/remote acceptance tests and local/remote shipped-binary smoke pass. Existing remaining-command assertions were preserved; no MCP test changes or instrumentation retained.
- AC3: extracted Claims UI tests pass, including renew/release previews, authorization, stale polls, and mouse behavior. Actual bare-binary PTY interaction verified Claims screen, keyboard help, returning to the empty claims view, and q exit, with no queue tab.
- AC4: TestLegacyArtifactsDoNotAffectClaims passes with old config/index/journal artifacts; doctor lists their paths as deletable while claim lifecycle works.
- AC5: queue-only fsnotify dependency removed. Final byte-identical standalone mise run ci passes format, staticcheck, vet, normal/race suites, vulnerability scan, binary and remote smoke, documentation, and man-page generation. Post-merge main lint/format/typecheck/test/doc-test also pass. Final fresh main race run (go test -race -count=1 -p 1 -parallel 2 ./...) passes in 134.83 seconds versus the original 1117.53 seconds on this host: 87.9% lower wall time.
- AC6: CHANGELOG Breaking section points to github.com/brettinternet/backlog.
- Renewal correction: unchanged heartbeat regression passes 100 repetitions; targeted heartbeat and hard-link safety tests pass race-enabled three-run checks. Actual hard links still fail closed.
- Single direct review completed; fixed the extracted mouse press-only guard. Requested review subagent was unavailable under model restrictions; no second general review was started.
- Implementation commits 62a1dbc and ca94d6a are fast-forward integrated into local main, with installed pre-commit hooks passing; nothing pushed.
- Verified original receipt, clean branch, and exact wHR workspace shell before Worktrunk removal. Worktree and task-156-claims-only branch removed together; personal post-remove hook closed wHR and absence verified. Owned standalone benchmark/diagnostic/PTY snapshots moved to Trash after ownership verification. No task worktree, workspace, or scratch checkout retained.

Review 2026-10-22: removed queue-era unused Claims UI palette styles (accent, good, bad, held, alert), renamed remote-smoke test env var QUEUE_TEST_HIDDEN_INVITE to SMOKE_TEST_HIDDEN_INVITE, and merged a duplicate empty CHANGELOG Unreleased heading. Verified legacy artifact paths match v1.9.3 config/index/recovery/handle locations, the store Nlink==0 fix keeps hard-link fail-closed behavior, and run supervisor still keeps claims for unresolved guarded operations. go test -race -count=3 claimsui and remote-smoke pass; mise run lint passes.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed the work queue while preserving claim commands, MCP claim tools, remote authority, and the bare Claims TUI. Added harmless legacy-artifact diagnostics and breaking migration guidance to bl. Fixed the exposed SQLite sidecar-unlink renewal race without weakening safety checks or existing tests. Full CI and post-merge checks pass; final fresh race wall time is 134.83 s versus 1117.53 s (87.9% lower). Code commits 62a1dbc and ca94d6a merged into local main; owned worktree, branch, workspace, and scratch snapshots cleaned. No push.
<!-- SECTION:FINAL_SUMMARY:END -->
