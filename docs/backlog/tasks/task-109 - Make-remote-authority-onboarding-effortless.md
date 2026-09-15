---
id: TASK-109
title: Make remote authority onboarding effortless
status: To Do
assignee: []
created_date: '2026-09-15 21:19'
updated_date: '2026-09-15 21:56'
labels:
  - remote-authority
  - ergonomics
dependencies: []
priority: high
type: feature
ordinal: 147000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A first-time administrator currently needs server initialization, deployment YAML, TLS provisioning, authority-ID discovery, profile creation, and bare-secret invite enrollment. Default-profile selection exists but is a separate step. Simplify this without changing local coordination or weakening remote trust.

Target journey: opt into guided server init, start serve, securely transfer one bootstrap artifact to the first administrator, and enroll with one command. That administrator can issue write-role artifacts for subsequent clients. Generated self-signed TLS is trusted through the transferred certificate pin, never trust on first use. Bare server init retains its existing local-only behavior; the documented remote journey explicitly selects guided setup.

Scope is TASK-109.1 through TASK-109.4 in dependency order. This parent is an integration checklist, not another implementation lane; complete it only after all four children and the clean-state journey pass. Certificate automation/rotation, hosted deployment management, and new coordination semantics are outside this change.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A fresh administrator can configure and start a standard remote authority without manually editing configuration files or obtaining certificates from an external CA.
- [ ] #2 A fresh client can establish trust, create its profile, and enroll with one command and one securely transferred artifact.
- [ ] #3 The happy path never requires curl, raw protocol headers, `--json` parsing, manual authority-ID discovery, or a separate profile-add step.
- [ ] #4 Secure transport and authority pinning remain fail-closed: self-signed server certificates are pinned through the invite rather than trusted on first use, and insecure LAN testing requires an explicit, clearly warned opt-in.
- [ ] #5 After enrollment the recipient runs lifecycle commands without repeating `--profile` on every command.
- [ ] #6 The documented two-machine happy path is covered by executable acceptance testing from clean state.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Complete server setup and freeze its endpoint/certificate handoff (TASK-109.1).
2. Implement artifact enrollment and profile activation (TASK-109.2).
3. Extend existing remote doctor checks (TASK-109.3).
4. Exercise and publish the two-machine journey (TASK-109.4), then verify parent criteria against that evidence.
<!-- SECTION:PLAN:END -->
