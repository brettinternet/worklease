---
id: TASK-64
title: Add an optional MCP interface for coding agents
status: To Do
assignee: []
created_date: '2026-09-11 18:25'
labels: []
dependencies: []
references:
  - README.md
  - docs/cli-reference.md
  - docs/claim-model.md
  - skills/worklease-workflow/SKILL.md
  - src/worklease/__init__.py
priority: medium
type: enhancement
ordinal: 68000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Provide an optional local MCP server for persistent, tool-native coding-agent sessions. It should reduce repeated process startup, shell quoting, and output-parsing overhead while preserving Worklease’s existing lifecycle and safety semantics. The `worklease` CLI and public Python API remain the canonical interfaces and the recovery/debugging path.

Design constraints:
- Ship the MCP integration separately from the dependency-free core so installing `worklease` does not add MCP dependencies.
- Use the public Python API directly against the same `WORKLEASE_HOME`; do not invoke the CLI as a subprocess or create a second lease authority.
- Start with local stdio transport and one coding-agent client per server process. Remote HTTP transport is out of scope.
- Expose typed, schema-versioned lease tools and preserve stable Worklease reason values, guarantee fields, exact resource ordering, and unknown-outcome behavior.
- Keep bearer tokens and lease-file contents entirely server-side. Return only a session-scoped lease reference plus non-secret claim metadata. Retained handles must use private storage and must not permit path traversal or cross-authority lookup.
- An optional heartbeat manager may renew a lease only while its owning MCP process/session remains active. It stops on disconnect or shutdown, never releases automatically, and never continues an orphan indefinitely.
- Do not add provider discovery, provider writes, dependency scheduling, arbitrary command execution, or `replace-file` tools. MCP does not turn local coordination into provider-side or cross-host fencing.
- CLI and MCP clients using the same authority must contend for the same resource and remain interoperable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A separately installable MCP distribution or extra provides a documented local stdio server without adding runtime dependencies to the core `worklease` installation.
- [ ] #2 Typed tools cover resource-key derivation and the singleton acquire, status, heartbeat, checkpoint, release, operation-inspection, and reconciliation lifecycle.
- [ ] #3 Typed tools cover the equivalent ordered bundle lifecycle, preserving exact caller order and all-or-nothing contention behavior.
- [ ] #4 The server calls the supported public Worklease Python API directly and uses the same configured authority as the CLI; tests prove CLI-to-MCP and MCP-to-CLI contention and lifecycle interoperability.
- [ ] #5 Successful and failed tool results are schema-versioned structured data that preserve stable reason values, claim revisions, guarantee declarations, and unknown-outcome/reconciliation semantics.
- [ ] #6 No MCP result, error, log, diagnostic, or checkpoint exposes a bearer token or private lease-handle contents; automated tests cover success and failure redaction.
- [ ] #7 Server-issued lease references resolve only within the configured authority, are stored with private permissions when persisted, reject traversal or malformed references, and cannot silently adopt a claim using agent identity alone.
- [ ] #8 If automatic heartbeat is enabled, tests prove renewal occurs before half the TTL, stops when the owning session/server ends, does not auto-release, and allows the claim to expire after heartbeat ownership is lost.
- [ ] #9 Disconnect, cancellation, restart, stale revision, expired claim, and unknown-operation recovery behavior is documented, including when operators must use the CLI.
- [ ] #10 Documentation includes coding-agent MCP configuration, a complete safe lease lifecycle, guarantee-scope warnings, and explicit exclusions for provider workflow and arbitrary guarded execution.
- [ ] #11 A repeatable benchmark compares a multi-operation MCP lifecycle with equivalent JSON CLI subprocess calls and records latency and process-startup results without making an unsupported performance guarantee.
<!-- AC:END -->
