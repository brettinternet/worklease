---
id: TASK-64
title: Add an optional MCP interface for coding agents
status: Done
assignee:
  - '@brett'
created_date: '2026-09-11 18:25'
updated_date: '2026-09-11 19:54'
labels: []
dependencies: []
references:
  - README.md
  - docs/cli-reference.md
  - docs/claim-model.md
  - skills/worklease-workflow/SKILL.md
  - src/worklease/__init__.py
  - src/worklease/lease_file.py
  - src/worklease/cli_dispatch.py
  - src/worklease/schemas/v1/commands.json
  - pyproject.toml
modified_files:
  - CHANGELOG.md
  - README.md
  - benchmarks/mcp_lifecycle.py
  - docs/mcp.md
  - packages/worklease-source-sdk/pyproject.toml
  - packages/worklease-source-sdk/src/worklease_source_sdk/__init__.py
  - pyproject.toml
  - src/worklease/__init__.py
  - src/worklease/cli_dispatch.py
  - src/worklease/instructions.py
  - src/worklease/mcp_server.py
  - src/worklease/operations.py
  - src/worklease/schemas/v1/index.json
  - src/worklease/schemas/v1/mcp.json
  - tests/test_mcp.py
  - tests/test_package_smoke.py
  - tests/test_release.py
  - tests/test_schemas.py
  - tests/test_store.py
  - uv.lock
priority: medium
type: enhancement
ordinal: 68000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Provide an optional local MCP server so a coding agent can run a full lease lifecycle as typed tool calls instead of shell subprocesses. The server exists to remove the three things agents get wrong with the CLI: forgetting to heartbeat, mishandling tokens and revisions, and mis-quoting a ~40-option command line. The `worklease` CLI and public Python API remain canonical and are the recovery path.

## Tool surface (7 tools)

Singleton and bundle lifecycles are one tool set. `acquire` and `status` take `resources: string[]` (1–32, exact caller order). The server routes a one-element list to the singleton API and a longer list to the bundle API so CLI callers on the same authority see the same claim kind and contend normally. The returned lease reference records the kind, so `heartbeat`, `checkpoint`, and `release` need no bundle variant.

| Tool | Inputs | Annotations |
| --- | --- | --- |
| `key` | `provider`, `source`, `item`, `coordination_only` | read-only, idempotent |
| `acquire` | `resources`, `ttl`, `work_key`, `coordination_only`, `wait_timeout` (≤ 60 s), `auto_heartbeat` (default true), `max_hold` | — |
| `status` | `resources`, `verbose` (default false) | read-only |
| `list` | optional resource filter | read-only |
| `heartbeat` | `lease`, `ttl` | — |
| `checkpoint` | `lease`, `checkpoint` (JSON object), `ttl` | — |
| `release` | `lease`, `reason` | destructive |

Excluded from MCP and served by the CLI only: `exec`, `exec-bundle`, `replace-file`, `transfer`, `history`, `gc`, `inspect-operation`, `reconcile-operation`, policy commands, provider discovery, provider writes, dependency scheduling, and any HTTP transport. In-process calls cannot lose a response, so unknown outcomes arise only from a crash between authority commit and handle write, or from CLI-started guarded operations under the same claim. `status --verbose` lets the agent detect those; the operator reconciles with the CLI using the lease handle path below.

## Lease references

A lease reference is an opaque server-generated identifier for a mode-0600 lease-file handle written with the existing `lease_file` module under `$WORKLEASE_HOME/mcp-leases/` (directory mode 0700). Tokens and revisions never leave that file. The server validates the reference against a strict identifier grammar and confirms the resolved path stays inside the directory before any read. A reference resolves across server restarts within the same authority because it is a capability, not an identity; possession of the reference is equivalent to possession of the CLI lease file. Release unlinks the handle only after the authority confirms. Operators recover a stuck claim with `worklease release --lease-file "$WORKLEASE_HOME/mcp-leases/<lease>.lease" --reason ...`.

## Identity

`agent_id` comes from `WORKLEASE_AGENT_ID` or a server startup argument. `session_id` is generated once per server process; the MCP server lifetime is the session. `owner_id` is fresh per `acquire`. `work_key` defaults to the resource or ordered resources, as in the CLI. Adoption by identity alone is impossible: a new session gets a fresh claim unless it presents an existing lease reference.

## Automatic heartbeat

`acquire` starts a renewal task by default. It renews before half the TTL elapses, holds the lease's in-process mutation lock while renewing, only ever renews leases acquired by the current server process (never handles found on disk at startup), stops on stdin EOF, cancellation, or shutdown, never releases, and stops after `max_hold` (default 4 hours) so a forgotten live session cannot hold a resource all day. Results report `expiresAt` and `autoHeartbeat: active | stopped | disabled`. Mutations on one lease reference are serialized in-process so concurrent tool calls fail with the stable reason rather than racing.

## Results and errors

`structuredContent` is the schema-v1 JSON envelope the CLI emits with `--json` and `--lease-file`, minus the token, plus `lease` and `autoHeartbeat`, and each tool declares it as `outputSchema`. Text content is a one-line summary; verbose diagnostics are opt-in. Every failure, including `already-claimed`, `stale-claim`, `invalid-token`, `claim-expired`, and invalid input, returns `isError: true` with the same non-secret error fields the CLI emits (stable `reason`, holder metadata, hint). Protocol-level JSON-RPC errors are reserved for unknown tools and malformed requests. stdout carries protocol only; logging goes to stderr under the same redaction rules.

The server's `instructions` string carries the `worklease instructions loop` and `instructions safety` text so the agent gets the safety contract without a discovery call.

## Packaging

Ship as the `mcp` extra with a `worklease-mcp` console script. Core `worklease` stays dependency-free. The PyInstaller release binary does not include the server; document `uv tool install` from a tag with the extra and the matching MCP client configuration.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 An `mcp` extra and `worklease-mcp` console script provide a documented local stdio server; the core `worklease` distribution keeps zero runtime dependencies and the PyInstaller binary is unchanged.
- [x] #2 Exactly the seven tools `key`, `acquire`, `status`, `list`, `heartbeat`, `checkpoint`, and `release` are exposed, with tool annotations and descriptions; no exec, replace-file, transfer, history, gc, reconcile, policy, or provider tools exist.
- [x] #3 `acquire` and `status` accept 1–32 ordered resources; a one-element list produces a singleton claim and a longer list produces a bundle claim; tests prove CLI-to-MCP and MCP-to-CLI contention, status visibility, and lifecycle interoperability for both kinds on one authority.
- [x] #4 The server calls only the supported public Python API, resolves the authority with the same `--home` / `WORKLEASE_HOME` / XDG precedence as the CLI, and never spawns the CLI.
- [x] #5 `structuredContent` is the schema-v1 CLI JSON envelope minus the token plus `lease` and `autoHeartbeat`, declared as `outputSchema`; tests validate every success and failure result against the published v1 schemas; failures return `isError: true` with stable `reason` values.
- [x] #6 Lease handles are written with the existing `lease_file` module under `$WORKLEASE_HOME/mcp-leases/` (dir 0700, files 0600); references match a strict identifier grammar; traversal, malformed, and foreign-authority references are rejected; a reference resolves across a server restart; a fresh session without a reference gets a fresh claim.
- [x] #7 No result, error, text summary, log line, or checkpoint contains a bearer token or lease-file contents; automated tests assert redaction on acquire, heartbeat, checkpoint, release, and every failure path including `invalid-token`.
- [x] #8 Automatic heartbeat renews before half the TTL, is serialized with explicit mutations on the same lease, renews only leases acquired by the current process, stops on stdin EOF, cancellation, shutdown, and `max_hold`, never releases, and the claim expires after the server exits; each case has a test.
- [x] #9 `wait_timeout` is capped at 60 seconds and documented as short contention backoff, with guidance to select other work rather than block.
- [x] #10 The server `instructions` string is the `instructions loop` and `instructions safety` output, sourced from the same generator so it cannot drift from the CLI.
- [x] #11 Documentation covers MCP client configuration for at least Claude Code, a complete safe lifecycle, guarantee-scope warnings, the recovery matrix (disconnect, restart, stale revision, expiry, unknown outcome) naming the CLI lease-file command for each operator step, and the explicit exclusions.
- [x] #12 A repeatable benchmark runs one acquire / heartbeat / checkpoint / release lifecycle over MCP and over equivalent `--json --lease-file` subprocess calls, recording wall latency, process count, and the byte size of `tools/list` and each result payload as a context-cost proxy, without asserting a performance guarantee.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add an optional MCP SDK extra and a stdio-only worklease-mcp entry point while keeping core dependencies and native packaging unchanged.
2. Implement a seven-tool server over the public Worklease lifecycle API, with secure persisted lease references, token-redacted schema-v1 results, strict validation, and shared CLI-generated instructions.
3. Add automatic heartbeat lifecycle management, per-lease mutation serialization, bounded wait behavior, restart recovery, and stale-handle cleanup.
4. Add interoperability, security/redaction, schema, heartbeat/shutdown, tool-surface, and benchmark coverage.
5. Document installation, client configuration, safe lifecycle, guarantees, exclusions, recovery, and benchmark use.
6. Run focused checks and all project quality gates, independently review and verify acceptance, update/finalize TASK-64, bump the minor version, commit, push main, tag, publish the GitHub release, and monitor CI/release workflows until green.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the optional stdio MCP server with a low-level seven-tool protocol surface, public Worklease API integration, persisted private capability handles, singleton/bundle checkpoint support, strict schema-v1 results, automatic heartbeat rearming and lifecycle shutdown, documentation, and benchmark.

Verification: all project tests passed (301 core and 19 SDK); lint, format-check, typecheck, lock check, and diff check passed. Eighteen focused MCP tests cover real stdio, CLI interoperability, token and malicious-checkpoint redaction, reference safety, contention, restart, wait bounds, heartbeat timing/rearming/concurrency/max-hold/cancellation/shutdown/EOF, and published schemas. The lifecycle benchmark completed with one MCP server process versus four CLI processes. Independent reviewer findings were fixed; independent verifier recheck passed.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @brett
created: 2026-09-11 18:34
---
Decision (2026-09-11): reconciliation and inspect-operation stay CLI-only; MCP lease references persist as lease-file handles and resolve across server restarts. Rationale: in-process calls cannot lose responses, so unknown outcomes are operator recovery cases; persisted handles give a CLI release path after a crashed session. Safeguard: the server removes handles whose claim is free or expired when serving `list`, so mcp-leases does not accumulate dead files.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added the optional worklease-mcp stdio server and mcp extra for complete singleton and bundle lease lifecycles without exposing tokens or revisions. Added restart-safe private lease references, bounded automatic heartbeats, a published MCP schema, bundle checkpoints, Claude Code/recovery documentation, and a repeatable transport benchmark. Verified by 320 project tests, all lint/format/type checks, real stdio and CLI interoperability tests, package/native build checks, and independent acceptance verification.
<!-- SECTION:FINAL_SUMMARY:END -->
