---
id: TASK-115
title: Report truthful recovery errors for remote pending requests
status: To Do
assignee: []
created_date: '2026-09-16 17:51'
labels: []
dependencies: []
references:
  - internal/authority/http.go
  - internal/cli/remote_lifecycle.go
  - internal/cli/lease_commands.go
  - docs/remote-claim-authority.md
priority: high
type: bug
ordinal: 157000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Remote mutation preparation can detect a pending-request collision before any network dispatch, but the client currently collapses the underlying failure into storage-failure with commitState unknown. In the reproduced expired-claim flow, heartbeat reported operation-request-mismatch and acquire reported “secondary remote request could not be durably recorded,” leaving no actionable distinction between a changed request, a local persistence failure, and a genuinely uncertain remote effect. Recovery output must preserve the safest specific cause, state whether dispatch occurred, and tell the operator which exact action is permitted next.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A failure to durably stage a secondary request before network dispatch reports commitState not-committed and preserves the stable underlying reason instead of converting every failure to storage-failure.
- [ ] #2 Failures after possible dispatch continue to report commitState unknown and retain the exact pending request; no error path weakens uncertain-outcome safety.
- [ ] #3 A lifecycle command encountering a pending acquire reports that the pending acquire must be recovered, rather than surfacing an unexplained operation-request-mismatch for the attempted heartbeat, checkpoint, or release.
- [ ] #4 Errors include a bounded recovery hint that distinguishes replaying the exact pending request, acquiring a fresh epoch after definitive expiry, and using --session only for a separate independent loop.
- [ ] #5 Text and JSON contract tests cover pre-dispatch persistence errors, request mismatches, definitive remote rejection, uncertain transport outcomes, and exact-request replay without exposing credentials or private request payloads.
<!-- AC:END -->
