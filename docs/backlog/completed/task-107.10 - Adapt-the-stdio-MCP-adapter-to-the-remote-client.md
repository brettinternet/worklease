---
id: TASK-107.10
title: Adapt the stdio MCP adapter to the remote client
status: Done
assignee:
  - '@brett'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 11:28'
labels:
  - remote-authority
dependencies:
  - TASK-107.8
references:
  - internal/mcp
  - docs/mcp.md
  - TASK-85.15
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
modified_files:
  - internal/authority/authority.go
  - internal/authority/handle_pending.go
  - internal/authority/http.go
  - internal/cli/commands.go
  - internal/lease/service.go
  - internal/mcp/lifecycle.go
  - internal/mcp/mcp.go
  - internal/mcp/renew.go
  - internal/mcp/server.go
  - internal/mcp/remote_test.go
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 142000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Adapt the local stdio MCP adapter to the shared authority interface in parallel with the CLI. Preserve the exact existing 11 tools: `key`, `acquire`, `status`, `list`, `heartbeat`, `checkpoint`, `verify`, `watch`, `events`, `release`, and `instructions`. This task does not add exec, replace, reconciliation, history, inspection, enrollment, or administrative tools. CLI-only operations remain CLI-only.

`key` and `instructions` remain pure local tools and make no network request even when a remote profile is selected. Local key derivation may return a host-local key and scope, but remote acquire rejects host-local `path:`, `backlog-md:`, and `markdown:` keys with `resource-not-enrolled`. The adapter selects the same trusted local or remote profile for authority operations and keeps installation credentials, claim credentials, handles, and exact pending requests local. Existing authority tools use remote behavior and return structured authentication guidance, but no MCP call enrolls an installation or redeems an invite.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The existing 11 tools, `key`, `acquire`, `status`, `list`, `heartbeat`, `checkpoint`, `verify`, `watch`, `events`, `release`, and `instructions`, use the shared local or remote authority interface with unchanged input schemas and compatible structured results.
- [x] #2 The task adds no MCP tools for exec, replace-file, history, reconciliation, private inspection, enrollment, invitation, revocation, GC, reopening, restore, or retirement; those operations remain CLI-only where supported.
- [x] #3 `key` and `instructions` remain pure local tools and make no network request even with a selected remote profile. Key derivation may return host-local keys and scope; a remote acquire of those keys fails `resource-not-enrolled`. Handles, credentials, exact pending state, automatic renewal, and watch loops stay on the MCP client host.
- [x] #4 The normal existing tools work remotely with authority-time scheduling, persisted hold caps, bounded watch long polls, cursor-gap reporting, exact pending recovery, and fail-closed incarnation handling.
- [x] #5 Missing and revoked installation credentials return distinct `authentication-required` and `installation-revoked` guidance naming the external CLI profile or enrollment action. No tool call enrolls, redeems an invite, or opens a browser.
- [x] #6 Tool results, logs, and MCP configuration expose no installation credential, invite, claim token, private recovery evidence, or server-side executable input.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reuse trusted profile selection for the MCP command and construct either the local or remote shared authority without changing the 11 tool schemas.
2. Route acquire, status/list, lifecycle mutations, verify, events/watch, and automatic renewal through authority.Authority while retaining all lease handles, exact pending requests, hold caps, and renew loops locally.
3. Add remote MCP integration tests for tool parity, local-only key/instructions, authority-time/recovery/watch behavior, authentication guidance, and secret redaction.
4. Run focused tests, review the diff, then run all repository quality gates and commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Selected as the earliest dependency-ready item. Acquired local Worklease claim 096a36f4105277a2742852633b7a39b0 on the Backlog.md item resource; provider writes remain local-coordination and are re-read after updates.

Implemented shared-authority routing for all authority-backed MCP tools while preserving the exact 11-tool surface and local-only key/instructions behavior. Remote handles now durably retain automatic-renewal intent and hold caps; remote lifecycle calls use authority-clock upper bounds, bounded 30-second watch polls, exact pending replay, definitive-failure pending cleanup, and credential preflight guidance.
Verification passed: focused remote MCP integration tests; mise run lint; mise run format-check; mise run test; mise run typecheck. Independent review found five remote edge cases (contended acquire recovery, definitive failure cleanup, stale authority time, 60-second watches, and missing-credential mutation preflight); all were fixed and relevant regression coverage was added.

The initial lease expired during the long independent review; reacquired ownership as claim 2e30c40c25b13099ee32385ca4e1e915 and re-read the authoritative task before final delivery.

Delivery commit b11014e passed mise run ci (format-check, generated man page, staticcheck, go vet, e2e, full tests, race tests, and govulncheck).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Adapted the stdio MCP server to the selected local or remote shared authority without changing its 11-tool contract. Remote operation state, credentials, hold caps, renewal, recovery, and watch loops remain client-local; authentication failures provide CLI enrollment guidance and public results remain redacted. Verified by remote MCP integration/regression tests, independent review, all focused quality gates, hooks, and mise run ci on commit b11014e.
<!-- SECTION:FINAL_SUMMARY:END -->
