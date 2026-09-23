---
id: TASK-109
title: Make remote authority onboarding effortless
status: Done
assignee:
  - '@brett'
created_date: '2026-09-15 21:19'
updated_date: '2026-09-16 03:26'
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
- [x] #1 A fresh administrator can configure and start a standard remote authority without manually editing configuration files or obtaining certificates from an external CA.
- [x] #2 A fresh client can establish trust, create its profile, and enroll with one command and one securely transferred artifact.
- [x] #3 The happy path never requires curl, raw protocol headers, `--json` parsing, manual authority-ID discovery, or a separate profile-add step.
- [x] #4 Secure transport and authority pinning remain fail-closed: self-signed server certificates are pinned through the invite rather than trusted on first use, and insecure LAN testing requires an explicit, clearly warned opt-in.
- [x] #5 After enrollment the recipient runs lifecycle commands without repeating `--profile` on every command.
- [x] #6 The documented two-machine happy path is covered by executable acceptance testing from clean state.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Complete server setup and freeze its endpoint/certificate handoff (TASK-109.1).
2. Implement artifact enrollment and profile activation (TASK-109.2).
3. Extend existing remote doctor checks (TASK-109.3).
4. Exercise and publish the two-machine journey (TASK-109.4), then verify parent criteria against that evidence.

5. Re-run the clean-state onboarding journey and all repository gates on main, independently review the parent criteria, then record the integration result.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Post-delivery review of the TASK-109 commit range found and fixed three defects (commit 5b2e290): (1) a fresh 'server init' could adopt a bootstrap secret staged beside the invite file by an unrelated or interrupted run, making a previously transferred bearer the new authority's admin; it is now refused before the home is marked, with guided preflight coverage, while same-authority resume still reuses its own staged secret; (2) definitively rejected remote mutations leaked durable pending records until the 256-record bound failed valid mutations locally; RemoteAuthority.Execute now finalizes definitive rejections and retains only uncertain outcomes; (3) guided setup did not sync parent directories after creating or clearing its journal, config, certificate, and key. Regression tests added in internal/cli and internal/authority; verified failing before each fix. Revalidated: mise run lint, format-check, test, typecheck, hooks, doc-test, remote-smoke, remote-smoke-vm.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed the TASK-109 integration checklist. Guided pinned-TLS server setup, self-contained one-command enrollment, default-profile lifecycle use, explicit insecure opt-in, and the documented clean-state two-machine journey are all delivered by TASK-109.1 through TASK-109.4. Local and real-host acceptance harnesses, documentation validation, repository quality gates, and independent verification all passed with no findings.
<!-- SECTION:FINAL_SUMMARY:END -->
