---
id: TASK-122
title: Clarify implicit session and handle-selector semantics
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-16 23:57'
updated_date: '2026-09-17 04:46'
labels:
  - cli
  - docs
  - ux
dependencies: []
references:
  - internal/cli/lease_commands.go
  - docs/claim-model.md
  - docs/cli-reference.md
  - internal/config/config.go
  - internal/cli/remote_commands_test.go
priority: medium
type: enhancement
ordinal: 164000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Omitting both --session and configured session selection uses an empty-selector contextual slot while acquire generates a claim sessionId. docs/claim-model.md already distinguishes these concepts, but help and diagnostics need a consistent explanation: the generated claim sessionId is metadata, not a lookup key for the original slot. Extend the existing contract rather than changing session generation or credential selection. A distinct selector selects another handle, not another resource-lock namespace.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 CLI help and lifecycle documentation name the contextual handle selector separately from claim session metadata
- [ ] #2 Commands and diagnostics represent an omitted selector consistently as unscoped and explain when a claim sessionId is generated
- [ ] #3 User-visible acquire, status, list, and contention output does not imply that a generated claim sessionId can recover a contextual handle or bypass exact-resource contention
- [ ] #4 Examples demonstrate stable explicit session use across acquire, heartbeat, status, and release, plus profile switching and same-resource contention
- [ ] #5 Tests cover omitted and explicit selectors and verify stable, unambiguous text and structured output terminology
- [ ] #6 Unscoped means the resolved selector is empty after flag/environment/config precedence, not merely that --session was omitted; documented examples cover WORKLEASE_SESSION_ID and explicit override.
- [ ] #7 This change preserves existing JSON field names and claim sessionId values, session generation, handle selection, and exact-resource contention semantics; terminology changes do not turn the display label unscoped into a literal selector.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Audit help, doctor, lifecycle text, and existing session docs; use contextual selector for lookup and claim sessionId for recorded metadata.
2. Explain configuration precedence and label the empty resolved selector unscoped without changing the empty-string selection key or existing JSON sessionId values.
3. Add explicit-session lifecycle and same-resource contention examples now; describe current profile-switch limitations and let TASK-119 update its resulting behavior.
4. Add omitted/environment/config/explicit-selector assertions and verify generated IDs cannot be used to rediscover the original slot. Preserve existing machine-readable fields.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Validation: internal/cli/lease_commands.go generates a random session only when cfg.SessionID is empty; acquireHandlePath passes the configured selector to ContextualPath. internal/config/config.go also resolves WORKLEASE_SESSION_ID and config-file session values, so an omitted flag is not necessarily unscoped. docs/claim-model.md already explains generated epoch metadata and uncertain-request recovery. TestRemoteCLIDefaultAndExplicitSessionsSelectIndependentHandles covers slot separation but not all terminology. TASK-119 profile-switch examples should land with the authority-scoping change; this terminology task need not wait for that implementation.

Implemented selector/session terminology across help, lifecycle text, diagnostics, and docs. Added coverage proving the unscoped empty selector remains distinct from generated claim session metadata, explicit flags override WORKLEASE_SESSION_ID, and separate selectors still contend on one exact resource. Focused internal/cli and internal/doctor tests pass.
<!-- SECTION:NOTES:END -->
