---
id: TASK-124
title: Make local a first-class profile selection
status: To Do
assignee: []
created_date: '2026-09-17 23:00'
updated_date: '2026-09-17 23:03'
labels:
  - ergonomics
  - remote-authority
dependencies: []
references:
  - docs/remote-claim-authority.md
  - docs/cli-reference.md
  - internal/config/profile.go
  - internal/cli/profile_commands.go
  - internal/cli/authority_context.go
  - internal/config/profile_test.go
  - internal/cli/profile_commands_test.go
priority: medium
type: enhancement
ordinal: 166000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Remote enrollment can make a remote profile the user default. Returning to local authority should not require editing profiles.yaml, deleting enrollment, or passing --local forever. Keep local as a reserved built-in authority selection in the existing profile UX, not a persisted remote Profile with fabricated endpoint or credentials.

Design contract:
- Reserve the exact, case-sensitive name local. Persist explicit user intent as default: local or a checkout binding value of local in the existing owner-private files; never add a local entry to the persisted profiles collection. An absent default remains implicit local fallback, distinct from an explicit local default.
- Preserve precedence: --profile > WORKLEASE_PROFILE > checkout binding > user default > implicit local. local is accepted at every selection layer and stops fallback just like a remote name. Explicit local uses Profile=nil, Name=local and the selecting layer as Source; implicit fallback keeps Source=local. No second authority resolver or new config file is needed.
- Keep --local as the existing force-local escape hatch: it bypasses bindings/defaults and profile-store loading, and conflicts with any nonempty --profile or WORKLEASE_PROFILE, including local. This intentionally retains the current conflict rule rather than making it a synonym for --profile local. Report this forced choice distinctly from implicit fallback in inspection output.
- profile default local changes only the user default, not checkout bindings. profile bind local overrides a remote user default, but not a flag/environment selection. unbind removes the override and restores fallback; it does not mean select local.
- Local is available without setup and cannot be added, enrolled, or removed as a remote profile. Reject reserved names before discovery, invitation redemption, credential writes, or config mutation, including invite artifact profile hints. Remote-only operations fail clearly when local is selected; they must not fall through to a retained remote profile.
- Existing persisted remote profiles named local are a compatibility collision, not an invitation to silently route their users to local authority. Fail closed when loading such a store with an actionable migration diagnostic; leave all files and credentials untouched. Document a manual rename of the remote profile and its default/binding references, retaining its existing credential path. --local remains available as the existing explicit bypass. No automatic rename/migration command is in scope.

Inspection must distinguish configured default from effective selection. profile default without NAME reports the configured value (including explicit local), retaining the existing unset/null behavior when no default is configured. profile show without NAME succeeds for local and reports why it was selected; profile show local inspects the built-in without changing selection. Text and JSON identify local without remote endpoint, authority pin, or credential fields. Preserve existing remote JSON fields and shapes; expose built-in metadata additively rather than serialize a fake remote Profile.

Non-goals: changing enrollment auto-default behavior, changing local storage scope, adding named local stores, changing checkout-root resolution, introducing repository-controlled authority configuration, or changing remote trust/credential semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Selection tests cover local at flag, environment, binding and default layers, plus implicit fallback, with competing remote lower-priority selections. Each resolves to local with correct name/source and without constructing a remote client or reading remote credentials; unknown names remain errors.
- [ ] #2 profile default local round-trips an explicit default: local without creating a persisted local profile, changing bindings, or altering retained remote profiles/credentials. profile default distinguishes explicit local from an unset default; --profile NAME still selects a retained remote profile.
- [ ] #3 profile bind local [--cwd DIR] persists the existing canonical checkout-root binding and overrides a user remote default. Flag/environment selections still win, other checkouts are unchanged, and unbind restores normal fallback.
- [ ] #4 profile list/ls always exposes the built-in local selection separately from persisted remote profiles. profile show local and unqualified profile show work for local without setup or network access; text/JSON distinguish built-in identity, effective selection source, and configured versus unset default while retaining existing remote JSON shapes.
- [ ] #5 profile add local, enrollment targeting local (explicitly or via artifact hint), and profile remove local fail with actionable errors and no discovery/redemption requests, credential writes or config changes. Remote-only operations selected through local fail clearly rather than panic or fall back to remote.
- [ ] #6 --local continues to bypass bindings/defaults and unreadable/invalid profile stores, conflicts with any nonempty flag/environment profile selection including local, and remains network-free. Inspection distinguishes forced local from implicit local fallback.
- [ ] #7 Existing stores without reserved-name collisions retain their behavior. A persisted remote profile named local causes an actionable fail-closed migration error, never silent authority switching or mutation; migration documentation explains renaming the profile and its default/binding references while retaining the credential path. Tests cover this collision and the --local escape hatch.
- [ ] #8 CLI help and user/CLI documentation explain precedence, explicit versus implicit local, checkout overrides, unbinding, reserved-name errors and migration. Focused config/CLI tests cover text/JSON inspection and failure side effects, and shared authority resolution is verified for ordinary commands and MCP startup.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend the existing config selector and persistence validation to recognize explicit local selections without constructing a remote Profile; preserve implicit fallback and reject legacy reserved-name collisions safely. Keep selection-name validation separate from remote-profile creation validation.
2. Update profile management and shared CLI authority selection/inspection. Reject reserved enrollment/addition names before remote side effects, audit nil-Profile handling in remote-only commands and MCP startup, and retain --local bypass/conflict semantics.
3. Add focused precedence, persistence, inspection, compatibility and zero-side-effect regression tests. Use existing profile/config/CLI test patterns rather than a parallel resolver.
4. Update help and docs, then run the repository quality gates. Leave implementation acceptance unchecked until these behaviors are exercised.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Design refinement only: inspected internal/config/profile.go, internal/cli/profile_commands.go and internal/cli/authority_context.go. Current selection already uses nil Profile for local; current --local bypasses profile loading and conflicts with any explicit/environment name. ValidateProfileName currently allows local, including in invite artifacts, so reserving it requires an explicit compatibility policy. Implementation remains To Do.
<!-- SECTION:NOTES:END -->
