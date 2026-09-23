---
id: TASK-85.17
title: 'Replace documentation, skills, build, and release automation'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 20:57'
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
- [x] #1 The man generator derives all registered commands/flags/examples and embeds the selected version; render validation runs on the supported OS matrix using platform-appropriate man/roff commands.
- [x] #2 Built-binary smoke covers version/key, session-scoped acquire/verify/exec/checkpoint/release, path-covered replacement and stdio MCP with an isolated home.
- [x] #3 Go workflows prepare four correctly named archives with bin/worklease and share/man/man1/worklease.1 plus verified checksums; each target is installed/smoke-tested on its native CI runner without publication.
- [x] #4 Current docs, workflow skills and backlog doc-1 describe the amended Go contract; historical migration/deferred-design text is explicitly exempted from executable-example checks, and the remote proposal remains deferred.
- [x] #5 Marked runnable examples execute against temporary state, documentation validation distinguishes forbidden argv --token from supported --token-file/--token-fd, CHANGELOG describes cutover/disposal, and mise run ci-go passes.
- [x] #6 Separate concise human CLI and MCP/JSON quick starts execute against temporary state and cover the short common path, structured contention handling and two isolated loops. Advanced recovery and native hooks are linked references; CLI-only operations are clearly identified instead of promising eleven-tool MCP parity with the entire command tree.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add command-tree-derived Go man generation with versioned output, exhaustive command/flag/example tests, and platform render validation.
2. Add hermetic built-binary smoke coverage and Go archive/checksum preparation for linux/darwin amd64/arm64; wire mise and native CI validation without changing Python publication ownership.
3. Rewrite current README/CLI/claim/MCP and workflow-skill surfaces for the amended Go contract, update backlog workflow doc-1 only through backlog CLI, preserve the deferred remote proposal, and mark runnable versus historical examples explicitly.
4. Add executable documentation/release validation including human and MCP/JSON quick starts, forbidden argv token checks, archive layout, and CHANGELOG cutover/disposal notes.
5. Run focused checks, full repository quality gates and ci-go, perform an independent review, fix findings, then finalize and commit on main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented command-tree-derived Go man generation, built-binary smoke (lifecycle, exact path replacement, modern/legacy MCP), executable marked docs examples, structured contention/two-session checks, Go archive/checksum tooling, four-target native workflow validation, current Go docs/skills, and doc-1 update through backlog CLI. Transitional Python packages remain until TASK-85.18; retired SDK compatibility doc removed as required.

Final verification: mise run ci-go passed (format, vet/staticcheck, unit, race, vulnerability, built-binary smoke, executable docs, man generation); mandoc -Tlint dist/worklease.1 passed; local worklease-release built all four archives and tar listings confirmed bin/worklease plus share/man/man1/worklease.1; mise run lint, format-check, test, and typecheck passed. Independent review found three documentation/smoke defects; all were fixed and rechecked with go-doc-test.

Delivery commit: da6d272 (Prepare Go release and documentation). Final-commit mise run ci-go passed with a clean worktree.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Replaced current documentation and the provider-neutral workflow skill with the amended Go contract, removed the retired SDK compatibility guide, and updated backlog doc-1 through the CLI. Added command-tree-derived versioned man generation, CGO-disabled four-target archive/checksum tooling, native CI install/render/smoke validation, executable human/JSON/MCP quick starts, and built-binary lifecycle/path/MCP smoke. AC1: TestWriteManPageDerivesRegisteredCommandsFlagsExamplesAndVersion plus mandoc -Tlint and CI groff/mandoc steps. AC2: mise run go-smoke (version/key, session acquire/verify/exec/checkpoint/release, exact-path replace, modern/legacy MCP). AC3: TestCreateArchiveUsesStableInstallMembersAndVerifiedChecksums, TestReleaseTargetsCoverSupportedNativeArchives, local worklease-release all-target build/tar inspection, and release go-assets matrix. AC4: mise run go-doc-test plus full test_skills suite; deferred remote document remains and current docs exclude historical/deferred files from executable validation. AC5: go-doc-test validates marked temporary-state examples and credential-source rules; CHANGELOG updated; mise run ci-go passed. AC6: go-doc-test executes human, JSON two-session, structured contention, modern MCP discovery, and two MCP lease-reference loops; TestEndToEndDiscoveredClientUsesOnlyLeaseReference covers the eleven-tool boundary. Full mise lint, format-check, test, and typecheck also passed.
<!-- SECTION:FINAL_SUMMARY:END -->
