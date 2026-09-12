---
id: TASK-85
title: Rewrite Worklease in Go
status: To Do
assignee: []
created_date: '2026-09-12 03:21'
updated_date: '2026-09-12 05:53'
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
- [ ] #1 The Go binary provides every command and all eleven MCP tools in the amended contract with tested local coordination, session isolation, exact recovery and public redaction behavior.
- [ ] #2 Production CLI/MCP require no Python and build for all four POSIX targets without CGO, or the documented driver amendment provides the tested native build alternative.
- [ ] #3 Retired Python core/SDK/packaging are removed, every retained capability inventory row is delivered or explicitly rejected, and deferred remote design is preserved without implementing it.
- [ ] #4 Four release archives install and pass native smoke tests with checksums and man pages; code/artifact completion does not require tag creation or public release.
- [ ] #5 All eighteen implementation/inventory children are Done with evidence, final Go gates pass, and CHANGELOG documents the intentionally incompatible cutover and optional recoverable state disposal.
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @codex
created: 2026-09-12 05:53
---
TASK-86 amends the contract under the owner-requested-change rule in section 15. The review preserves Go/POSIX/SQLite and one claim model, removes Python parity requirements, tightens session/handle isolation, authenticated recoverable replay, operation-specific protection, predecessor recovery, cursor/watch/GC semantics and MCP hold bounds, and preserves the Cloudflare proposal as deferred. All eighteen child tasks now use the amended decisions and live dependency graph; remote implementation and publication are outside this refinement.
---
<!-- COMMENTS:END -->
