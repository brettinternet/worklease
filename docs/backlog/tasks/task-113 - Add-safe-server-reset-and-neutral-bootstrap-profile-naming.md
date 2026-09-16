---
id: TASK-113
title: Add safe server reset and neutral bootstrap profile naming
status: In Progress
assignee:
  - '@brettinternet'
created_date: '2026-09-16 15:50'
updated_date: '2026-09-16 17:13'
labels: []
dependencies: []
priority: medium
type: enhancement
ordinal: 155000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A missing server configuration can leave a valid hosted home that blocks zero-flag initialization, while manual filesystem cleanup risks deleting the wrong authority or leaving credentials and generated artifacts inconsistent. The bootstrap invite also names the enrolled profile after its admin role, which becomes misleading when that name is propagated to non-admin enrollment. Provide an explicit safe reset path, make profile identity role-neutral, and publish the correction as the next patch release.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A confirmed server reset safely clears a valid hosted authority so server initialization can succeed again at the same home; preview/refusal paths do not mutate state
- [ ] #2 Reset refuses a running server, active claims, unsafe or ambiguous homes, and unresolved operations unless a redacted export is explicitly requested; it removes only a matching owner-private bootstrap artifact and leaves deployment config/TLS intact
- [ ] #3 The non-empty-home initialization error gives an actionable reset command for the resolved home, and initialization recognizes the safely retired hosted-home shape
- [ ] #4 Bootstrap artifact enrollment uses the neutral profile hint remote, activates it as the default when appropriate, and text output displays the installation role separately
- [ ] #5 CLI help, user documentation, and focused tests cover reset safety, reinitialization, and neutral profile naming
- [ ] #6 Version 1.6.1 is committed, pushed, tagged, published, and verified with passing local and remote release checks
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add server reset as a stricter sibling of retirement: resolve and preview the home, take the hosted lock, always refuse active claims, require an external redacted export for forced unresolved state, verify/remove only the matching bootstrap artifact, and clear hosted database readiness while preserving config/TLS and the fencing marker. 2. Allow guided initialization to reuse only the known safely retired hosted-home shape and make the current non-empty-home failure print the exact reset command. 3. Change bootstrap invite profile hints to remote, include the enrolled role in text output, and update focused tests/help/docs. 4. Run focused and full gates, obtain independent safety review, prepare the 1.6.1 changelog, commit, push main, validate CI/release, tag v1.6.1, and verify published artifacts.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented reset command, retired-home reinitialization, neutral remote profile hints with legacy artifact upgrade, role-separated enrollment output, pinned no-replace retirement exports, bootstrap credential binding, staged-secret refusal, readiness-first restartable cleanup, focused safety tests, and operator docs. Initial independent review found export collision/aliasing, stale secret carryover, crash ordering, artifact upgrade, and removal race defects; fixes are implemented and re-review is running.

Independent safety re-review passed after durable reset-intent recovery, current-bootstrap binding, readiness-before-artifact ordering, no-replace pinned exports, and atomic quarantine removal fixes. Local evidence: focused reset/hosted/handle tests pass; exact mise run test, lint, format-check, typecheck, vuln, e2e, race, doc-test, release-note extraction, staged hooks, and GIF regeneration/visual contact-sheet inspection pass. One full CI aggregate race run hit an existing watch timing flake; the focused test passed 10/10 and an immediate exact mise run race passed.
<!-- SECTION:NOTES:END -->
