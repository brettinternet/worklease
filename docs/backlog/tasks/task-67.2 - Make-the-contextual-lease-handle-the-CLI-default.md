---
id: TASK-67.2
title: Make the contextual lease handle the CLI default
status: To Do
assignee: []
created_date: '2026-09-12 02:04'
updated_date: '2026-09-12 02:05'
labels:
  - cli
  - ux
  - security
dependencies:
  - TASK-67.1
references:
  - src/worklease/cli.py
  - src/worklease/cli_dispatch.py
  - src/worklease/lease_file.py
  - src/worklease/schemas/v1/commands.json
  - scripts/release_docs.py
  - tests/test_cli.py
  - CHANGELOG.md
parent_task_id: TASK-67
priority: high
type: enhancement
ordinal: 75000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Wire the resolution module from TASK-67.1 into the CLI so the contextual handle is the default and the path-free lifecycle in TASK-67 works end to end. Precedence, collision, opt-out, payload field, and error-reason names are settled in the TASK-67 description; read it before planning and do not redesign them.

Touchpoints in `src/worklease/cli.py`: `_add_lease_file_argument`, `_common_claim_arguments`, `_common_bundle_claim_arguments`, the `status` and `status-bundle` parsers, `_resolve_lease_file`, `_lease_file_destination`, `_validate_lease_file_destination`, `_validate_claim_arguments`, `_lease_file_requested`, `_persist_lease_file`, `_suppress_lease_file_token`, `_emit_runtime_error_hint`, and the command epilogs that currently show `--lease-file "$LEASE_FILE"`. The short option `-L` is unused today. `schemas/v1/commands.json` needs the new `leaseFile` payload field. `scripts/release_docs.py` renders the man page from the live parser, so verify it still runs. Leave `mcp_server.py` untouched; it keeps its own `mcp-leases/` handles. Leave `history` untouched; TASK-69 owns bare `history`.

Watch for: the `lease-file-in-use` check must run before the store mutation commits, as it does today; a failed handle write after commit must keep emitting the token-bearing payload with exit 75; idempotent replays must not roll the handle revision backwards.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `acquire` and `acquire-bundle` with neither `-L` nor `--no-lease-file` write the contextual handle (mode 0600 inside the mode 0700 `context-leases/` directory under the resolved state home), omit `token` from JSON and text output, report the handle path as `leaseFile` (`LEASE_FILE` in text), and create no file in the repository or caller directory.
- [ ] #2 `heartbeat`, `checkpoint`, `exec`, `transfer`, `reconcile-operation`, `release`, and their bundle variants run with no identity or credential flags by reading the contextual handle; successful mutations rewrite it, `transfer` without `--successor-lease-file` rewrites it with the successor claim, and a successful release removes only that handle.
- [ ] #3 `status` and `status-bundle` accept `-L`/`--lease-file PATH`, infer resource(s) from it or from the contextual handle when `--resource` is omitted, prefer explicit `--resource`, and fail `lease-file-kind-mismatch` with a hint naming the matching command when the handle kind does not match; `history` keeps requiring `--resource` (bare `history` is reserved for TASK-69).
- [ ] #4 Precedence matches TASK-67: explicit `-L` keeps the existing per-field override; complete explicit identity plus exactly one credential runs stateless without reading or writing the contextual handle; a partial mixture fails `lease-context-conflict` (exit 64) with a hint listing the required set; a required but absent handle fails `lease-context-missing` (exit 64) with a hint naming the context root and `acquire`.
- [ ] #5 `-L PATH` is accepted as an alias of `--lease-file PATH` on every command that accepts `--lease-file`, and the existing `--lease-file` and `--successor-lease-file` tests pass without modification.
- [ ] #6 `acquire --no-lease-file` and `acquire-bundle --no-lease-file` return the current stateless payload including `token`; combining `--no-lease-file` with `-L` or `--lease-file` exits 64 with an actionable hint.
- [ ] #7 A second implicit acquire while the contextual handle names a live claim fails `lease-file-in-use`; the error and text hint identify the handle path and context root, point to `release` or `-L PATH`, and contain no token; a handle whose claim is inactive is replaced.
- [ ] #8 Missing, malformed, unsafe, symlinked, wrong-kind, oversized, unwritable, and stale contextual handles surface the existing `lease-file-*` reasons or the new `lease-context-*` reasons without token material and never fall back to another credential mode.
- [ ] #9 Help text and epilogs for the affected commands show the path-free lifecycle and mention `-L PATH` and `--no-lease-file`; `worklease --help-all` and `scripts/release_docs.py` render without errors; `schemas/v1/commands.json` documents `leaseFile`.
- [ ] #10 CHANGELOG `Unreleased` has a Breaking entry stating that a bare `acquire` now writes a contextual handle and omits the token, naming `--no-lease-file` as the compatibility path, and noting the next release is 0.10.0.
- [ ] #11 Tests cover singleton and bundle default lifecycles, Git root and subdirectory invocation, linked-worktree isolation, non-Git scoping, `--home` and `WORKLEASE_HOME` overrides, every precedence branch, both opt-outs, collision, permissions, token suppression, each diagnostic, release cleanup, and unchanged explicit lease-file and stateless behavior; `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
