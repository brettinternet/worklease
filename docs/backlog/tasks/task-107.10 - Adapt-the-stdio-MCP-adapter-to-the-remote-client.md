---
id: TASK-107.10
title: Adapt the stdio MCP adapter to the remote client
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies:
  - TASK-107.9
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
`worklease mcp` stays a local stdio server so MCP hosts need no remote transport or OAuth support. It resolves the same profile configuration as the CLI and calls either the local service or the remote client; installation credentials, claim credentials, handles, and pending requests stay on the client host and MCP exposes only opaque lease references. Automatic renewal and watch long polls must work without putting secrets in MCP configuration or model-visible results. Enrollment never happens from a tool call: a missing or revoked credential returns structured guidance to enroll outside MCP, distinguishing `authentication-required` from `installation-revoked` so an agent can report the right next step.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every MCP tool works against a remote profile with the same input schemas and structured error details as locally; no tool result, log line, or MCP configuration contains an installation credential, claim token, or invite.
- [ ] #2 Automatic renewal uses the remote client authority-time scheduling and `holdUntil` caps; watch tools use the bounded long poll and expose cursor gaps as they do locally.
- [ ] #3 A missing credential returns `authentication-required` and a revoked one returns `installation-revoked`, each with guidance naming the CLI enrollment step; no tool call initiates enrollment, redeems an invite, or opens a browser.
- [ ] #4 Project-scoped MCP setup binds the project root or explicit profile; a user-global MCP process never infers a repository from request content; guarded execution stays client-local and remote `replace-file` is disabled with a structured reason.
- [ ] #5 MCP acceptance tests cover remote acquire, renew, and release, lost-response recovery through pending requests, and `authority-restored` fail-closed behavior.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
