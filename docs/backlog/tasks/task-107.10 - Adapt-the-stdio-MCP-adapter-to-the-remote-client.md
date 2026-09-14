---
id: TASK-107.10
title: Adapt the stdio MCP adapter to the remote client
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:03'
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
- [ ] #1 The existing 11 tools, `key`, `acquire`, `status`, `list`, `heartbeat`, `checkpoint`, `verify`, `watch`, `events`, `release`, and `instructions`, use the shared local or remote authority interface with unchanged input schemas and compatible structured results.
- [ ] #2 The task adds no MCP tools for exec, replace-file, history, reconciliation, private inspection, enrollment, invitation, revocation, GC, reopening, restore, or retirement; those operations remain CLI-only where supported.
- [ ] #3 `key` and `instructions` remain pure local tools and make no network request even with a selected remote profile. Key derivation may return host-local keys and scope; a remote acquire of those keys fails `resource-not-enrolled`. Handles, credentials, exact pending state, automatic renewal, and watch loops stay on the MCP client host.
- [ ] #4 The normal existing tools work remotely with authority-time scheduling, persisted hold caps, bounded watch long polls, cursor-gap reporting, exact pending recovery, and fail-closed incarnation handling.
- [ ] #5 Missing and revoked installation credentials return distinct `authentication-required` and `installation-revoked` guidance naming the external CLI profile or enrollment action. No tool call enrolls, redeems an invite, or opens a browser.
- [ ] #6 Tool results, logs, and MCP configuration expose no installation credential, invite, claim token, private recovery evidence, or server-side executable input.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
