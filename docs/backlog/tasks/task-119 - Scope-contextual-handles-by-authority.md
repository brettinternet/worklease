---
id: TASK-119
title: Scope contextual handles by authority
status: To Do
assignee: []
created_date: '2026-09-16 23:57'
updated_date: '2026-09-17 00:08'
labels:
  - cli
  - handles
  - remote
dependencies: []
references:
  - internal/handle/handle.go
  - internal/cli/lease_commands.go
  - docs/claim-model.md
  - docs/cli-reference.md
  - internal/cli/guard_commands.go
  - internal/handle/handle_test.go
  - internal/cli/remote_commands_test.go
priority: high
type: bug
ordinal: 161000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Contextual handle paths currently depend on checkout root and session selector but not authority identity, even though every handle is authority-bound. Switching between the embedded local authority, a remote profile, or a reinitialized server can therefore select an unrelated handle and fail with authority-mismatch instead of using the intended authority safely.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The same checkout and session selector can hold independent contextual handles for the local authority and multiple remote authorities without collisions
- [ ] #2 Switching profiles selects the handle for that authority, and switching back resumes the original matching handle
- [ ] #3 A reinitialized authority does not overwrite, retarget, or silently discard a handle from the previous authority identity
- [ ] #4 Legacy contextual handles migrate or resolve only after their authority identity matches; mismatched and pending legacy state remains recoverable
- [ ] #5 Locking, owner-only permissions, exact pending-request recovery, CLI/MCP selection boundaries, documentation, and focused migration tests cover the new selection behavior
- [ ] #6 Profiles with the same authorityId share one contextual slot; a changed endpoint or profile name does not create a new slot. A changed restoreId does not silently bypass existing pending recovery or authority-restored handling.
- [ ] #7 Explicit --handle, WORKLEASE_HANDLE, stateless credentials, and MCP lease references retain their existing selection boundaries; guarded commands use the same authority-scoped contextual selection as lifecycle commands.
- [ ] #8 Legacy/new destination conflicts never overwrite either file; migration preserves exact pending and recovery records under existing race-safe locks, and missing-authority read-only selection creates neither a store nor a handle.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Define contextual namespace as canonical checkout root + resolved selector + authorityId, not profile name, endpoint, credentials, or restoreId. Preserve explicit handle and MCP lease-reference precedence.
2. Centralize selection for lifecycle and guarded-effect paths. Resolve local identity without turning read-only/missing-authority commands into store creation; use configured remote identity without extra network discovery solely for path selection.
3. Add locked legacy resolution that never overwrites a destination or hides ambiguous recovery state. Preserve pending payloads and credentials exactly; conflicting legacy/new handles fail with actionable explicit-path recovery guidance.
4. Test local/remote switches, profile aliases, reinitialization versus restore, legacy conflicts, concurrency, guard paths, and unchanged MCP-private selection; update path documentation.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Validation: internal/handle/handle.go ContextualPath hashes only checkout root and selector. internal/cli/lease_commands.go acquireHandlePath uses it before authority-bound handle validation; internal/cli/guard_commands.go also computes a contextual path independently. This confirms collisions, not an authority-fencing bypass. Profiles are aliases, not authority identities. Server restore changes restoreId without changing authorityId; do not confuse restore with a fresh authority initialization. Existing reference: TestContextRootAndContextualPathAreStableAndSessionScoped and TASK-114 remote reacquisition coverage.
<!-- SECTION:NOTES:END -->
