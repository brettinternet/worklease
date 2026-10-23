---
id: TASK-85.18
title: Cut over to Go and retire the Python proof of concept
status: done
type: chore
priority: high
assignees: ['@brett']
labels: [go-rewrite]
milestone: m-0
parent: TASK-85
blocked_by: [TASK-85.17]
rank: ac0397cdb2c039724
refs: [docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md, pyproject.toml, uv.lock, src/worklease, tests, packages/worklease-source-sdk, scripts, mise.toml, .github/workflows/ci.yml, CLAUDE.md, TASK-67, TASK-74, TASK-75, TASK-76, TASK-77, TASK-78, TASK-79, TASK-80, TASK-81, TASK-82, TASK-83, TASK-84]
created: 2026-09-12T03:24:00Z
updated: 2026-09-12T21:31:00Z
---

Retire the Python proof of concept after the Go capability inventory is satisfied and artifacts are validated. Own Python-era removal, Go generic quality gates/hooks and final integration/review. Read contract sections 14–16 and 19–21; preserve historical backlog records and the deferred remote proposal.

Remove the exact tracked Python core/SDK/tests/packaging/build assets using recoverable cleanup. Repoint lint/format/test/typecheck/ci to Go and update AGENTS/CLAUDE guidance, preserving managed blocks. Historical Python references are allowed in committed task evidence and migration notes; do not erase evidence to satisfy a broad grep.

Verify duplicate Python-era tasks stay superseded. Run clean Go-only builds and native end-to-end scenarios, obtain independent review of filesystem safety, process cleanup, transaction/replay/recovery boundaries, redaction and cancellation, and resolve blocking findings. Release publishing remains a separately authorized action.

Evidence and patterns (the amended contract is normative): the Python-era surfaces to remove are `pyproject.toml`, `uv.lock`, `src/worklease`, `tests/*.py`, `packages/worklease-source-sdk`, `scripts/*.py`, the Python jobs in `.github/workflows/ci.yml`, and the Python tools and tasks in `mise.toml`. The TASK-85.1 inventory document lists every retained capability and its owner; TASK-74 through TASK-84 are already Done as superseded and TASK-67 shipped in Python.

## Acceptance criteria

- [x] Tracked Python runtime/SDK/build/test assets and Python mise/CI jobs are retired; historical backlog/migration evidence and docs/distributed-cloudflare-claim-authority.md remain, with no remote implementation.
- [x] Generic repository gates and hooks pass with Python unavailable to the build/test commands, and ci includes Go race/smoke checks without bypassing installed hooks.
- [x] A committed clean-checkout end-to-end script covers multi-resource/session claims, path-covered replacement, supervised exec, pending recovery, predecessor reconciliation, history/events/watch/GC/doctor/setup and MCP on Linux/macOS.
- [x] Every retained/redesigned inventory capability is delivered or explicitly rejected with rationale; no duplicate Python-era task remains selectable, and the full command/tool surface is verified.
- [x] An independent review report records concrete findings and resolution evidence; TASK-85 receives final acceptance evidence for code/artifacts without requiring unauthorized publication.

## Definition of done

- [x] `mise run ci` passes in a clean checkout with no Python on PATH
- [x] Final summary names the commands and review report that prove each acceptance criterion

## Plan

1. Audit the Go rewrite against the cutover contract and capability inventory.
2. Remove tracked Python runtime, SDK, tests, packaging, build, mise, and CI surfaces while preserving historical evidence and the deferred remote proposal.
3. Add or complete a committed Go-only clean-checkout end-to-end scenario and repoint repository gates/hooks to Go race and smoke checks.
4. Run focused and full quality gates with Python unavailable, independently review the required safety boundaries, resolve findings, and commit.
5. Record objective acceptance evidence in TASK-85.18 and TASK-85, then finalize TASK-85.18.

## Notes

Cutover removed the tracked Python runtime, SDK/plugin, schemas, tests, benchmark, packaging, release scripts, lockfiles, and Python CI/release jobs. Generic mise tasks and Lefthook now target Go; CI executes hooks and the full Go race/vulnerability/end-to-end gate. Added scripts/test-e2e.sh and expanded built-binary smoke across multi-resource claims, replacement, exec, operations, read views, watch, GC, doctor, setup, policy, and MCP; focused native tests cover pending/predecessor recovery, redaction, cancellation, session isolation, and all eleven tools. Independent review found one high-severity CI hook-execution gap; it was fixed with hooks-all and verified at exit 0. Review report: docs/reviews/task-85.18-independent-review.md.

The first installed-hook commit run exposed inherited GIT_INDEX_FILE breaking nested temporary-worktree tests. Lefthook now unsets that transient Git variable before mise run test; this preserves the staged-file decision while isolating child Git repositories.

## Summary

Completed the Go-only cutover. AC1: git asset audit confirms Python runtime/SDK/schema/test/benchmark/packaging/release files and Python CI/release jobs are deleted while docs/distributed-cloudflare-claim-authority.md remains. AC2/DoD1: mise run lint, format-check, test, typecheck, hooks-all, and mise run ci passed; ci ran with failing python/python3 shims first on PATH and includes race, vulnerability, smoke/E2E, and installed-hook execution. AC3: scripts/test-e2e.sh passed, combining built-binary and documentation journeys with focused pending/predecessor/recovery/redaction/cancellation/all-tool acceptance tests. AC4: go test ./... and command/MCP acceptance tests passed; Backlog listing confirms TASK-74 through TASK-84 remain Done/superseded. AC5: docs/reviews/task-85.18-independent-review.md records the independent review, its one high-severity hook finding, the fix, and passing resolution evidence; TASK-85 comment #5 records final code/artifact evidence without publication.
