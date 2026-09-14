---
id: TASK-85
title: Rewrite Worklease in Go
status: Done
assignee:
  - '@brett'
created_date: '2026-09-12 03:21'
updated_date: '2026-09-14 01:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.18
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - ../hum/cmd/hum/main.go
  - ../hum/internal/config/config.go
  - ../hum/internal/cli/root.go
  - ../hum/internal/mcp/server.go
  - src/worklease
  - tests
modified_files:
  - docs/backlog/tasks/task-85 - Rewrite-Worklease-in-Go.md
priority: high
type: feature
ordinal: 92000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Rewrite Worklease as a maintainable POSIX Go coordinator for concurrent agent loops. Python is behavior evidence only: no compatibility is required for API/SDK/plugins, state, schemas, commands, outputs or implementation structure. Preserve useful capabilities and simplify around one claim model.

The Go Product Contract is normative. TASK-86 refines ownership/replay/guard/handle/event semantics and preserves docs/distributed-cloudflare-claim-authority.md as future design. V1 ships only the local authority. Typed services, explicit authority identity and honest resource/operation capabilities are the current preparation; remote transport, credentials, deployment and fencing counters are deferred.

Use the actual dependency graph, not static waves. TASK-85.1 inventories capabilities before bootstrap. This parent remains the closure task after 85.18. Publication, pushing and PRs are separately authorized actions; a completed local rewrite must not invent publication authority. Python-era tasks 74–84 remain closed as superseded.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The Go binary provides every command and all eleven MCP tools in the amended contract with tested local coordination, session isolation, exact recovery and public redaction behavior.
- [x] #2 Production CLI/MCP require no Python and build for all four POSIX targets without CGO, or the documented driver amendment provides the tested native build alternative.
- [x] #3 Retired Python core/SDK/packaging are removed, every retained capability inventory row is delivered or explicitly rejected, and deferred remote design is preserved without implementing it.
- [x] #4 Four release archives install and pass native smoke tests with checksums and man pages; code/artifact completion does not require tag creation or public release.
- [x] #5 All eighteen implementation/inventory children are Done with evidence, final Go gates pass, and CHANGELOG documents the intentionally incompatible cutover and optional recoverable state disposal.
- [x] #6 The section 1.1 human CLI and MCP/JSON interaction priorities pass executable common-path, contention and isolated-loop journeys; the shipped quick starts require no manual credential/revision plumbing or mandatory guard setup.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Re-run the complete Go quality, acceptance, release-build, and native smoke-test gates on main.
2. Review the closure diff and verify all six parent acceptance criteria against executable evidence and completed child records.
3. Record evidence, complete TASK-85, stage the authoritative backlog update, run hooks, and commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Closure verification on main: mise run lint, format-check, test, and typecheck passed; mise run ci passed with unit, race, vet/staticcheck, govulncheck, built-binary E2E, executable documentation journeys, and man generation. worklease-release built all four CGO-disabled 0.10.0 POSIX archives; each archive contained bin/worklease and share/man/man1/worklease.1, generated checksums verified, and the installed macOS arm64 archive passed worklease-smoke. Backlog JSON confirmed all eighteen children Done with every child acceptance criterion checked. Reviewed the closure diff and cumulative TASK-85.14/.15/.17/.18 evidence; no unresolved item-scoped defect remains.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @codex
created: 2026-09-12 05:53
---
TASK-86 amends the contract under the owner-requested-change rule in section 15. The review preserves Go/POSIX/SQLite and one claim model, removes Python parity requirements, tightens session/handle isolation, authenticated recoverable replay, operation-specific protection, predecessor recovery, cursor/watch/GC semantics and MCP hold bounds, and preserves the Cloudflare proposal as deferred. All eighteen child tasks now use the amended decisions and live dependency graph; remote implementation and publication are outside this refinement.
---

author: @codex
created: 2026-09-12 06:07
---
Owner-requested amendment recorded in contract sections 1.1, 12 and 17 (TASK-86): prioritize short setup-free human commands and ergonomic handle-backed JSON/typed MCP orchestration. Preserve structured error details; test common paths, contention and isolated loops in 85.14–85.17. This does not expand the eleven MCP tools or implement remote authority.
---

author: @pi-01a09450
created: 2026-09-12 06:36
---
Capability inventory is complete in doc-3. It maps the discovered CLI, MCP, module, SDK, documentation, skill, and release surfaces to one Go owner each. No material contract gap or section 15 amendment was found; known Python differences are owned implementation work, and the Cloudflare design remains deferred.
---

author: @pi-01a09478
created: 2026-09-12 07:59
---
TASK-85.4 selected modernc.org/sqlite v1.58.0. The Go driver uses WAL, synchronous FULL, busy_timeout 10000, foreign_keys ON, immediate writes, deferred read-only observations, and one pooled connection. Driver tests prove serialization, rollback/cancellation, AUTOINCREMENT, crash durability, private main/WAL/SHM handling, and WAL-visible read-only access; four CGO-disabled targets build and govulncheck reports no findings. Limitation: modernc opens by path and cannot enforce O_NOFOLLOW, so TASK-85.6 must keep the home directory pinned and compare the opened device/inode. Commit errors are independently read back as committed, not-committed, or unknown.
---

author: @brett
created: 2026-09-12 21:30
---
TASK-85.18 final integration evidence: tracked Python runtime/SDK/packaging/test/release assets and Python CI/release jobs are removed; the deferred Cloudflare proposal remains documentation-only. mise run ci passed with failing python/python3 shims first on PATH, including Go test/race/vuln and clean-checkout E2E. mise run hooks-all passed after CI was changed to execute installed hooks. The full CLI registration and eleven MCP tools are covered by native acceptance tests and scripts/test-e2e.sh. Independent review and its resolved hook finding are recorded in docs/reviews/task-85.18-independent-review.md. No release was published.
---

created: 2026-09-12 23:46
---
Contract amendment (section 17, 2026-09-12): section 20 now recommends serving the existing Go authority over authenticated HTTPS instead of a Cloudflare Durable Object reimplementation, and names the restore generation and single-writer guard as pre-release remote invariants. Owner-authorized pivot; remote implementation stays deferred. Rationale in docs/distributed-cloudflare-claim-authority.md.
---

author: @C3
created: 2026-09-13 00:20
---
Correction after TASK-88: WAL-visible read-only access leaves the main database unchanged, but SQLite may update coordination words in an existing -shm sidecar and may recreate absent owner-private -wal/-shm sidecars in a writable directory. The amended Go Product Contract sections 8 and 13 and TestDriverReadOnlyWithoutSidecarsCreatesOnlyPrivateSidecars are authoritative.
---

author: brett
created: 2026-09-14 00:31
---
Owner-requested amendment recorded in contract sections 17 and 20 (no task): the deferred remote design now serves the Go authority behind a TLS-only edge with Worklease-owned invite-based installation authentication and read/write/admin roles, one namespace per serve process, portable keys with server-configured bounds, an immutable expectedRestoreId in every authenticated request plus restoreId on responses and cursors, a namespace recovery mode in place of a recovery-only claim type, a hosted-volume process-lifetime lock, and client pending state as recovery evidence. Rationale, rejected alternatives, and deferred follow-ups (repository enrollment, cross-host transfer, recovery import, client journal, backpressure, browser login, multi-namespace serve) are in docs/remote-claim-authority.md. No shipped behavior changes; remote implementation stays deferred.
---

created: 2026-09-14 01:06
---
Owner-authorized TASK-107 finalizes the remote authority contract while leaving shipped local behavior unchanged. Section 20 now requires exact pending recovery for every remote mutation and independent restore outcome and cessation coverage; the completed-history journal remains a triggered follow-up.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Closed the Go rewrite after all eighteen child tasks completed. The native CLI and eleven-tool MCP server, local authority, session-safe handles, exact recovery/redaction, Go-only build, four release archives, docs, quick starts, and deferred remote design are complete. Verified with mise run lint, format-check, test, typecheck, and ci; four-archive build/layout/checksums; installed native archive smoke; completed child records; and the resolved independent review in docs/reviews/task-85.18-independent-review.md. No tag, push, or release was performed.
<!-- SECTION:FINAL_SUMMARY:END -->
