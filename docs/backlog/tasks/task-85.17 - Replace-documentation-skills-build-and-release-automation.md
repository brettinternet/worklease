---
id: TASK-85.17
title: 'Replace documentation, skills, build, and release automation'
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 06:27'
labels:
  - go-rewrite
milestone: m-0
dependencies:
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
Replace documentation, skills, man generation and Go release preparation after all commands exist. Read contract sections 13–16, 19–21 and the capability inventory. Own the documented release/docs/skill surfaces and update backlog doc-1 only through the CLI. Keep Python packaging until 85.18.

Document the intentionally incompatible Go product, session-safe handles, no token output, operation-specific protection, exact path membership, replay/recovery and authority-bound events. Preserve docs/distributed-cloudflare-claim-authority.md as the deferred Go proposal; remove only retired SDK compatibility docs. The provider-neutral workflow stays provider-neutral while its Go adapter drops ownerId and bundle-specific concepts.

Build and test all four archives with the chosen driver, checksums and versioned man page. Release publication is a separate explicitly authorized action: do not create tags, push, dispatch publication or publish merely to satisfy a planning task. Local artifact and CI validation must not require a public release.

Evidence and patterns (the amended contract is normative): hum `.github/workflows/release.yaml` (CI verification, matrix build with ldflags, checksums, gh release), `cmd/hum-man/main.go` and `internal/cli/man.go` (man generation from the command tree), `README.md` structure, `scripts/install_test.sh`. Current repository evidence: `scripts/release_artifacts.py` (archive members `bin/worklease` and `share/man/man1/worklease.1`), `.github/workflows/release.yml` (asset naming), `README.md`, `docs/cli-reference.md`, `docs/claim-model.md`, `docs/mcp.md`, `skills/worklease-workflow/SKILL.md` and its references, backlog doc-1, `CHANGELOG.md`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The man generator derives all registered commands/flags/examples and embeds the selected version; render validation runs on the supported OS matrix using platform-appropriate man/roff commands.
- [ ] #2 Built-binary smoke covers version/key, session-scoped acquire/verify/exec/checkpoint/release, path-covered replacement and stdio MCP with an isolated home.
- [ ] #3 Go workflows prepare four correctly named archives with bin/worklease and share/man/man1/worklease.1 plus verified checksums; each target is installed/smoke-tested on its native CI runner without publication.
- [ ] #4 Current docs, workflow skills and backlog doc-1 describe the amended Go contract; historical migration/deferred-design text is explicitly exempted from executable-example checks, and the remote proposal remains deferred.
- [ ] #5 Marked runnable examples execute against temporary state, documentation validation distinguishes forbidden argv --token from supported --token-file/--token-fd, CHANGELOG describes cutover/disposal, and mise run ci-go passes.
- [ ] #6 Separate concise human CLI and MCP/JSON quick starts execute against temporary state and cover the short common path, structured contention handling and two isolated loops. Advanced recovery and native hooks are linked references; CLI-only operations are clearly identified instead of promising eleven-tool MCP parity with the entire command tree.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
