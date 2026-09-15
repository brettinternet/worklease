---
id: TASK-109.3
title: Add a remote onboarding verifier with actionable fixes
status: To Do
assignee: []
created_date: '2026-09-15 21:20'
updated_date: '2026-09-15 21:32'
labels:
  - remote-authority
  - ergonomics
dependencies:
  - TASK-109.2
parent_task_id: TASK-109
priority: high
type: enhancement
ordinal: 150000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
When remote setup fails, users fall through to protocol-level checks and must interpret strict HTTP errors themselves. Worklease already has a read-only `doctor` command for local checks; extend that existing surface with remote checks for the selected profile rather than adding a parallel diagnostics command, so there is one place to look when anything is wrong. The check chain must include the certificate pin introduced by TASK-109.2.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease doctor` with a selected remote profile verifies endpoint reachability, protocol compatibility, TLS trust or pinned-certificate match, pinned authority identity, enrollment state, role, and resource-prefix admission.
- [ ] #2 Human output leads with pass or fail and gives a concrete next command or configuration change for each recoverable failure.
- [ ] #3 JSON output exposes stable check identifiers and outcomes without secrets.
- [ ] #4 The checks distinguish listener, firewall, TLS, certificate pin, identity, credential, role, and admission failures.
- [ ] #5 Running the checks is read-only and cannot create profiles, enroll installations, or mutate claims.
- [ ] #6 Automated tests cover each reported failure class and confirm output redaction.
<!-- AC:END -->
