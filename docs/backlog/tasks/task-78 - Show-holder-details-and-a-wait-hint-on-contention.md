---
id: TASK-78
title: Show holder details and a wait hint on contention
status: To Do
assignee: []
created_date: '2026-09-12 02:18'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - src/worklease/acquisition.py
  - src/worklease/cli_dispatch.py
  - tests/test_cli.py
  - docs/cli-reference.md
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 85000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
When `acquire` loses to an existing lease, text output prints only `ERROR acquire: already-claimed` and the resource. The JSON envelope already carries a redacted holder claim (`LeaseError("already-claimed", resource=..., claim=...)` in `acquisition.py`), but `_emit_error_details` in `cli.py` only prints a flat allowlist, so humans cannot see who holds the lease or when it expires and agents get no pointer to `--wait-timeout`.

## Decision

Text rendering of `already-claimed` for `acquire` and `acquire-bundle` prints a holder block from the error's redacted claim using the existing claim field labels, limited to `claimId`, `agentId`, `workKey`, and `expiresAt`, followed by one `HINT` line suggesting `--wait-timeout SECONDS` (with `--poll-interval`) or selecting other ready work. `resource-guarded`, the other retryable acquire error in `_RETRYABLE_ACQUIRE_ERRORS`, gets the same hint. JSON output is unchanged.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Text `already-claimed` output from `acquire` and `acquire-bundle` prints, after `RESOURCE`, the holder `CLAIM_ID`, `AGENT_ID`, `WORK_KEY`, and `EXPIRES_AT` taken from the error claim, and never a token or checkpoint body.
- [ ] #2 Text `already-claimed` and `resource-guarded` output ends with one `HINT` line naming `--wait-timeout` and `--poll-interval` or choosing other ready work; the JSON error payload for both reasons is byte-identical to today.
- [ ] #3 `docs/cli-reference.md` text grammar documents the holder block and hint, and CHANGELOG `Unreleased` is updated.
- [ ] #4 Tests cover singleton and bundle contention text, the hint on both reasons, redaction, and unchanged JSON, and `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
