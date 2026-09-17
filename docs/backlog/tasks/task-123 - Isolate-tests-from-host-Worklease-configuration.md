---
id: TASK-123
title: Isolate tests from host Worklease configuration
status: To Do
assignee: []
created_date: '2026-09-17 00:15'
labels:
  - test
  - config
  - safety
dependencies: []
references:
  - internal/cli/doctor_commands_test.go
  - internal/cli/authority_context.go
priority: high
type: bug
ordinal: 165000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The test suite can read the operators real Worklease profiles because test helpers clear XDG variables to empty and leave configuration resolution able to fall back to the host home. With a selected remote profile, tests attempted network requests against that authority instead of their temporary local state, creating a risk of host-state reads or mutations and making results environment-dependent.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All tests and spawned test subprocesses use temporary HOME, XDG_CONFIG_HOME, and XDG_STATE_HOME directories rather than host paths
- [ ] #2 Test setup clears or overrides every Worklease configuration variable, including remote profile and server configuration selection
- [ ] #3 A regression test seeds sentinel host configuration and verifies that tests neither read it nor attempt network access to its endpoint
- [ ] #4 Test isolation applies consistently through mise run test and repository hooks without requiring callers to sanitize their shell
- [ ] #5 Focused tests and the full test suite pass when the real user configuration contains a selected remote profile
<!-- AC:END -->
