---
id: TASK-13
title: Add non-argv claim credential input
status: To Do
assignee: []
created_date: '2026-07-14 02:34'
labels:
  - cli
  - security
  - credentials
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
priority: medium
type: enhancement
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Let automation supply claim bearer tokens to mutating CLI commands without placing the secret value in process arguments or shell history. Preserve the one-time token returned by acquire and the existing token-redaction contract.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Heartbeat, release, exec, and replace-file accept documented file- and file-descriptor-based token sources with the same ownership semantics as the existing token argument.
- [ ] #2 Exactly one token source is accepted; missing, conflicting, unreadable, malformed, or unsafe credential inputs fail deterministically before state changes or child execution.
- [ ] #3 Credential values are never included in argv-derived diagnostics, JSON/text output, logs, exceptions, or child-process environments.
- [ ] #4 Existing success, stale-owner, idempotency, and exit-code behavior remains consistent across supported token sources, with automated redaction and child-isolation tests.
- [ ] #5 README lifecycle examples use a non-argv token source and explain the compatibility and security behavior of every supported source.
<!-- AC:END -->
