---
id: TASK-85.18
title: Cut over to Go and retire the Python proof of concept
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 04:47'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.17
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - pyproject.toml
  - uv.lock
  - src/worklease
  - tests
  - packages/worklease-source-sdk
  - scripts
  - mise.toml
  - .github/workflows/ci.yml
  - CLAUDE.md
  - TASK-67
  - TASK-74
  - TASK-75
  - TASK-76
  - TASK-77
  - TASK-78
  - TASK-79
  - TASK-80
  - TASK-81
  - TASK-82
  - TASK-83
  - TASK-84
parent_task_id: TASK-85
priority: high
type: chore
ordinal: 110000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
With the Go implementation documented and releasable, finish the intentionally incompatible rewrite: remove the Python core, SDK, packaging, tests, and tooling; point the generic quality gates at Go; verify that no Python-era backlog task remains selectable; and obtain an independent review of the riskiest properties before declaring the rewrite complete.

Read first: contract sections 2 (D16), 14, 16, 19, and the capability inventory document from TASK-85.1 (confirm every retain and redesign row has landed in a Done task). Evidence: `pyproject.toml`, `uv.lock`, `src/worklease`, `tests/`, `packages/worklease-source-sdk`, `scripts/*.py`, the Python jobs in `.github/workflows/ci.yml`, the Python tasks in `mise.toml`. The Python-era tasks TASK-74 through TASK-84 were already closed as superseded on 2026-09-12 and TASK-67 shipped in Python; this task verifies that state and handles anything created since.

Deliver: delete `src/worklease`, `tests/*.py`, `packages/`, `pyproject.toml`, `uv.lock`, `scripts/*.py`, `benchmarks/` if present, the `.venv` configuration, the Python tools and tasks in `mise.toml`, and the Python CI jobs; rename mise tasks so `lint` runs go-fmt-check and go-vet, `format-check` runs go-fmt-check, `format` runs go-fmt, `test` runs go-test and go-race, `typecheck` runs go-vet, and `ci` runs ci-go and go-smoke; make `hooks` and lefthook format staged Go files; update the CLAUDE.md quality-gate text if task names changed; run `backlog task list --exclude-status Done --plain` and for any nonterminal task outside TASK-85 that duplicates a TASK-85.x capability, use `backlog task edit` to mark it Done with a final summary "Superseded by TASK-85.x (Go rewrite)", or rewrite it for the Go design when a real gap remains, or leave it with an explicit non-duplicate rationale in its notes; request an independent review (the `reviewer` agent or an equivalent fresh-context review) targeting filesystem safety, process-group cleanup, transaction boundaries, secret redaction, goroutine leaks, and release rollback, and resolve every blocking finding; run the complete gates in a clean checkout where no python3 or python is on PATH.

Owned paths: everything Python-era being removed, `mise.toml`, `.github/workflows/ci.yml`, `CLAUDE.md`, any backlog tasks named above (through the CLI only). Out of scope: new features.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `git ls-files` shows no files under src/worklease, tests/*.py, packages/, or scripts/*.py and no pyproject.toml or uv.lock, and `rg -n "python|uv run|pyinstaller" mise.toml .github CLAUDE.md README.md docs` matches only historical changelog or migration-notice text.
- [ ] #2 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run ci` pass in a clean checkout where `command -v python3 python` finds nothing, and `mise run hooks` runs the Go formatter on staged files.
- [ ] #3 A clean-checkout end-to-end script committed under scripts/ installs a locally built archive and exercises acquire with a three-resource claim, verify, exec, replace-file, checkpoint, history, events, watch (timeout path), gc dry run and apply, doctor, setup mcp preview, and an MCP initialize, exiting 0 on Linux and macOS.
- [ ] #4 `backlog task list --exclude-status Done --plain` shows no task outside TASK-85 that duplicates a TASK-85.x capability: TASK-74 through TASK-84 remain Done as superseded, the TASK-67 family remains Done, and any task created since is Done with a "Superseded by" summary, rewritten for the Go design, or carries a notes entry with a specific non-duplicate rationale; the check and each decision are recorded in this task's notes.
- [ ] #5 The independent review report is attached as a task comment with every blocking finding resolved by a referenced commit, and the TASK-85 acceptance criteria are re-verified with evidence recorded on TASK-85.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes in a clean checkout with no Python on PATH
- [ ] #2 Final summary names the commands and review report that prove each acceptance criterion
<!-- DOD:END -->
