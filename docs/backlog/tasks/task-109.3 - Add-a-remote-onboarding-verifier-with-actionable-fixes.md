---
id: TASK-109.3
title: Add a remote onboarding verifier with actionable fixes
status: Done
assignee:
  - '@brett'
created_date: '2026-09-15 21:20'
updated_date: '2026-09-16 01:06'
labels:
  - remote-authority
  - ergonomics
dependencies:
  - TASK-109.2
references:
  - internal/cli/doctor_commands.go
  - internal/cli/doctor_commands_test.go
  - internal/server/handlers.go
  - internal/authority/http.go
parent_task_id: TASK-109
priority: high
type: enhancement
ordinal: 150000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Remote doctor already checks credential-file safety, metadata, authenticated list access, and recovery state in internal/cli/doctor_commands.go, but collapses most failures into generic messages. Extend that path rather than adding a second remote diagnostic command. Include TASK-109.2 certificate pins and distinguish observable failures without claiming that a timeout proves a firewall or listener fault.

Role and prefix checks need server evidence, not inference from a successful list request. Add only the minimal read-only authenticated self-inspection needed for the current installation role; do not expose all installations or require admin access. Public metadata may expose configured admitted prefixes, never credentials or installation identities. Preserve local doctor behavior and existing remote check IDs where their meaning is unchanged.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 doctor resolves profiles through existing selection precedence and checks reachability, protocol, TLS/leaf pin, authority/restore identity, enrollment and current role. With an optional resource key it evaluates that key against advertised prefixes; without a key it reports prefixes without claiming a particular resource is admitted.
- [x] #2 Human output leads with pass or fail and gives a concrete next command or configuration change for each recoverable failure.
- [x] #3 JSON uses the existing doctor success/error envelope with stable check IDs and ok/warn/fail outcomes; unavailable downstream checks are explicitly not verified, not reported as passing. Required failed checks return nonzero and all output is redacted.
- [x] #4 Checks distinguish observed DNS/connect/refused/timeout, TLS/pin, protocol/identity, missing/unsafe/revoked credential, role, and prefix failures. Timeout/refusal advice lists listener/firewall checks as possible causes, not proven diagnoses; network checks have bounded deadlines.
- [x] #5 Checks use only read-only requests and do not create or modify profiles, credentials, pending mutations, claims, or enrollment state. A read/write installation can inspect its own role without admin privileges; lack of admin recovery access is not an onboarding failure.
- [x] #6 Tests cover every reported failure class, unavailable dependent checks, read/write/admin roles, old servers missing optional diagnostic fields, deadlines, redaction, exit codes, and absence of local or authority mutations.
- [x] #7 The metadata response exposes admitted prefixes and resource-not-enrolled error details report them without secrets. Update typed decoding/protocol documentation together; absent optional fields on older servers produce an explicit unavailable diagnostic rather than fabricated role or admission results.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend the current remoteDoctorAction and reuse doctor.Check/output contracts; retain existing local checks.
2. Add minimal authenticated self-role inspection and optional public prefix metadata with explicit old-server behavior; update server routes, typed client decoding and protocol docs together.
3. Add optional resource admission evaluation, bounded probes and evidence-based corrective messages; test all roles, degraded servers and read-only behavior.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented typed admitted-prefix metadata and authenticated self-role inspection, actionable bounded remote doctor checks with optional resource admission evaluation, safe resource-not-enrolled details, protocol documentation, and focused compatibility/security tests. Focused Go tests pass for authority, server, CLI, and lease packages.

Validation: mise run lint, mise run format-check, mise run test, and mise run typecheck pass. Independent adversarial review found one unpinned-authority false-positive; fixed it and added regression coverage. Focused authority/server/CLI/lease tests pass after the fix.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Extended remote doctor with bounded evidence-based transport, TLS/pin, protocol/identity, credential, self-role, prefix, resource-admission, and recovery diagnostics. Added public admitted-prefix metadata, minimal authenticated self inspection, safe admission error details, old-server behavior, documentation, and broad automated coverage. Full repository quality gates pass; independent review finding was fixed and regression-tested.
<!-- SECTION:FINAL_SUMMARY:END -->
