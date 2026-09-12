---
id: TASK-85.17
title: 'Replace documentation, skills, build, and release automation'
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.14
  - TASK-85.15
  - TASK-85.16
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - ../hum/.github/workflows/release.yaml
  - ../hum/cmd/hum-man/main.go
  - ../hum/internal/cli/man.go
  - ../hum/scripts/install_test.sh
  - scripts/release_artifacts.py
  - .github/workflows/release.yml
  - README.md
  - docs/cli-reference.md
  - docs/claim-model.md
  - docs/mcp.md
  - skills/worklease-workflow/SKILL.md
  - docs/backlog/docs/worklease-workflow/doc-1 - Worklease-Workflow.md
parent_task_id: TASK-85
priority: high
type: chore
ordinal: 109000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Go implementation becomes the only supported distribution. Documentation, the workflow skill, agent instructions, CI, the man page, and reproducible release archives must all describe and ship the Go behavior with the same mise-compatible archive layout so existing `github:brettinternet/worklease` installs upgrade cleanly. Python packaging stays in place until TASK-85.18 removes it.

Read first: contract sections 2 (D12), 3 (documentation ownership), 13, 14, 16, 19. Patterns: `../hum/.github/workflows/release.yaml` (CI verification, matrix build with ldflags, checksums, gh release), `../hum/cmd/hum-man/main.go` and `../hum/internal/cli/man.go` (man generation from the command tree), `../hum/README.md` structure, `../hum/scripts/install_test.sh`. Evidence: `scripts/release_artifacts.py` (archive members `bin/worklease` and `share/man/man1/worklease.1`), `.github/workflows/release.yml` (current asset naming), `README.md`, `docs/cli-reference.md`, `docs/claim-model.md`, `docs/mcp.md`, `skills/worklease-workflow/SKILL.md` and its references, `docs/backlog/docs/worklease-workflow/doc-1 - Worklease-Workflow.md`, `CHANGELOG.md`.

Deliver: `cmd/worklease-man` and `internal/cli/man.go` generating worklease(1) from the command tree; mise tasks `go-man` and `go-smoke`; a built-binary smoke test (`cmd/worklease/integration_test.go`, hum pattern) covering version, key, acquire, verify, release with a temporary home, exec of a trivial command, and an MCP initialize plus tools/list round trip; `.github/workflows/release.yml` rewritten for Go per contract 14 (four archives, `checksums.txt`, version-matched man page, install-and-smoke of every archive on Linux and macOS runners, changelog excerpt as release notes); `README.md` rewritten around the Go quick start, contextual handles, and the one-claim model; `docs/cli-reference.md` rewritten from the Go tree; `docs/claim-model.md` updated to the contract identities and events; `docs/mcp.md` updated to `worklease mcp`, `setup mcp`, and the eleven tools; an install and upgrade section with the incompatible-cutover notice and Python-era state disposal instructions; `skills/worklease-workflow` updated to Go command names and the handle-by-default loop; `backlog doc update doc-1` for the workflow guide; `CHANGELOG.md` with a 1.0.0 section describing the incompatible rewrite; removal of `docs/source-provider-sdk-compatibility.md` and `docs/distributed-cloudflare-claim-authority.md` per contract 16; a `worklease.example.yaml`.

Owned paths: `cmd/worklease-man`, `internal/cli/man.go`, `cmd/worklease/integration_test.go`, `.github/workflows/release.yml`, Go-era scripts under `scripts/`, `README.md`, `docs/`, `skills/`, `CHANGELOG.md`, `worklease.example.yaml`, backlog doc-1 through the CLI. Out of scope: deleting Python sources or Python CI jobs (TASK-85.18).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise run go-man` generates a worklease(1) page listing every command from contract section 4 with its flags, the page renders without errors through `man -l dist/worklease.1` on Linux and macOS, and it embeds the version passed on the command line.
- [ ] #2 `mise run go-smoke` builds the binary and passes the built-binary integration test covering version, key, acquire, verify, and release with a temporary home, exec of a trivial command, and an MCP initialize plus tools/list round trip over stdio.
- [ ] #3 The release workflow, run through workflow_dispatch against a test tag, produces worklease-<version>-{linux,macos}-{x64,arm64}.tar.gz each containing bin/worklease and share/man/man1/worklease.1, a checksums.txt that `sha256sum -c` verifies, and installs and smoke-tests every archive on Linux and macOS runners (arm64 Linux on ubuntu-24.04-arm) with CGO_ENABLED=0 unless the TASK-85.4 amendment says otherwise.
- [ ] #4 README.md, docs/cli-reference.md, docs/claim-model.md, docs/mcp.md, the install and upgrade section, skills/worklease-workflow/SKILL.md with its references, and backlog doc-1 contain no Python commands, bundle-specific commands, --token, or lease-file paths (a documentation test greps them against a deny-list), and the CHANGELOG 1.0.0 section names the incompatible cutover and the state disposal instructions.
- [ ] #5 A documentation test executes every fenced shell example in README.md and docs/*.md that is preceded by an HTML comment marker `<!-- runnable -->` against a temporary home and asserts exit 0, and `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
