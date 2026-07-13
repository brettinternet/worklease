---
id: TASK-1
title: Extract and publish provider-neutral work-lease tool
status: To Do
assignee: []
created_date: '2026-07-13 19:25'
labels:
  - architecture
  - packaging
  - release
dependencies: []
priority: high
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Create a standalone work-lease project from the backlog-claim coordination concept. The public core must treat resource as an opaque caller-supplied identity and provide same-host SQLite/file-lock coordination only: acquire, status, list, heartbeat, release, expiry/reclaim, idempotency, revision/token checks, and guarded local process execution. Provider-specific resource-key derivation and fencing remain adapters rather than core behavior. Do not modify the existing ~/.dotfiles/ai/.bin/backlog-claim during this work item unless a later migration explicitly requires it. Use the existing mise.toml toolchain: Python 3.14, latest uv, and pyright; document development, test, and packaging commands through mise/uv (for example, mise exec uv -- uv sync and mise exec uv -- uv run ...).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A standalone uv-managed package installs and exposes a versioned work-lease CLI using the project mise.toml Python/uv/pyright toolchain.
- [ ] #2 The core treats resource identities as opaque and passes concurrency, expiry/reclaim, heartbeat, release, idempotency, revision, and stale-token tests without provider-specific code.
- [ ] #3 The public guarantee is explicitly documented as same-host SQLite/file-lock coordination; remote provider fencing is adapter-owned and requires provider-side conditional checks.
- [ ] #4 Provider-specific GitHub, Backlog.md, Markdown, and Linear behavior is isolated in optional adapters; generic execution cannot claim provider fencing.
- [ ] #5 JSON is the stable default, plaintext is explicit opt-in, and read-only list/status responses do not expose bearer tokens by default.
- [ ] #6 CI verifies concurrency, crash/expiry recovery, stale ownership rejection, package installation, pyright, and release artifacts.
- [ ] #7 A tested mise definition installs the GitHub release artifact, and release assets include checksums.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Scaffold the standalone Python package and CLI with uv, preserving the existing mise.toml toolchain and documenting mise/uv workflows.
2. Extract and test the provider-neutral lease state machine, SQLite migrations, same-host file locks, expiry/reclaim, heartbeat, release, idempotency, and guarded process behavior.
3. Define opaque resource identities and move GitHub, Backlog.md, Markdown, and Linear key/fencing behavior into optional adapters; generic leases report local-coordination only.
4. Stabilize the public CLI contract: version, schema version, exit codes, JSON default, opt-in plaintext, token redaction from read-only enumeration, and explicit same-host limitations.
5. Add CI for concurrent/crash/expiry/stale-token scenarios, package installation, pyright, and release validation.
6. Publish tagged GitHub release artifacts with checksums and a tested mise definition that installs the release artifact.
7. Update the dotfiles caller only in a separate migration task after the standalone tool is released.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Initial project setup is intentionally documented only. Leave ~/.dotfiles/ai/.bin/backlog-claim unchanged for now; migration/cutover is separate.
<!-- SECTION:NOTES:END -->
