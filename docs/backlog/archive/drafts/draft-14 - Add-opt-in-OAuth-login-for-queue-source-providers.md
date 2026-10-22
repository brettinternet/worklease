---
id: DRAFT-14
title: Add opt-in OAuth login for queue source providers
status: Draft
assignee: []
created_date: '2026-09-25 16:04'
updated_date: '2026-09-25 16:30'
labels:
  - work-queue
  - auth
dependencies:
  - TASK-142.2
references:
  - >-
    docs/backlog/tasks/task-134 -
    Work-queue-S8-evidence-driven-additions-intake.md
documentation:
  - docs/work-queue-tui-proposal.md
priority: low
type: feature
ordinal: 61000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Source credentials today come from `gh auth token` (GitHub) or, once TASK-134's Linear work lands, a user-configured helper command that prints a personal API key. Both work for one person at a workstation, but they push token creation, scoping, rotation, and revocation onto the user, and long-lived personal keys usually carry broader scopes than the queue needs. The user wants better authentication over time, starting with Linear and extending OAuth to as many source providers as possible.

Plan §10 already sets the rules: native-app OAuth is the fallback when a provider has no credential helper; no application secret is embedded; setup is read-only first; write scopes are requested only for authorized operations; token refresh is serialized per credential; unattended operations never prompt for login; secrets live in an OS keychain or protected store. This task turns those rules into working, opt-in OAuth login for each provider that can support it, and records why the others cannot.

Start after the built-in Linear adapter and its configured credential helper exist, so OAuth extends a working credential seam instead of replacing it. Candidate providers include every built-in remote source adapter (GitHub, Linear) and any provider later accepted through S8 intake (Jira, GitLab, ...). Backlog.md and loose Markdown are local and out of scope. This task may be split into one subtask per provider plus a shared login/token-store subtask when it is picked up.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 For every built-in remote source adapter and every provider accepted through S8 intake, docs/work-queue-tui-proposal.md §3 records the provider's OAuth evidence: public/native client support without an embedded secret (for example PKCE or device authorization), available scopes, token lifetime and refresh, revocation, and how the authenticated principal is verified. Each provider not supported gets a stated reason.
- [ ] #2 Each provider whose OAuth flow meets the §10 constraints can be authenticated for a configured source through an explicit interactive login, opt-in per source in queue.yaml.
- [ ] #3 Login requests read-only scopes first. Write scopes are requested only when the user enables a write capability, and capabilities are rediscovered afterward.
- [ ] #4 No client secret is embedded in the binary or configuration. Access and refresh tokens are stored only in the OS keychain or a protected owner-private store, and never appear in queue.yaml, argv, environment passed to launch actions or adapters, logs, receipts, the queue index, or claim metadata.
- [ ] #5 After login and after every refresh, the authenticated principal is verified against the source's configured account. A mismatch disables the source with a structured diagnostic.
- [ ] #6 Token refresh is serialized per credential across concurrent queue, CLI, and MCP processes, and a refresh race never loses a rotated refresh token.
- [ ] #7 Expired tokens, revoked grants, insufficient scope, SSO/SAML enforcement, rate limiting, and provider downtime each produce distinct structured diagnostics in TUI and JSON output.
- [ ] #8 Unattended operations (queue next --claim, MCP queue_next, launch handoffs) never start an interactive login and return an actionable authentication-required diagnostic instead.
- [ ] #9 Logout revokes the grant where the provider supports it, deletes stored tokens, and purges that principal's cached projections per §10, without deleting unresolved write-recovery records.
- [ ] #10 Existing credential paths (the gh helper and configured helper commands) keep working unchanged for sources that do not opt into OAuth.
- [ ] #11 Tests against a fake authorization server cover the login flow, refresh rotation and concurrent refresh, revocation, scope denial, principal mismatch, unattended refusal, and secret-leak checks across output and logs.
- [ ] #12 The decision register (§2), §10, §11, and user-facing queue docs describe the supported providers, flows, scopes, and storage.
<!-- AC:END -->
