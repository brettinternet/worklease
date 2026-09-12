---
id: TASK-78
title: Show holder details and a wait hint on contention
status: To Do
assignee: []
created_date: '2026-09-12 02:18'
updated_date: '2026-09-12 02:25'
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
When `acquire` loses to an existing lease, text output prints only `ERROR acquire: already-claimed` and the resource. The JSON envelope already carries a redacted holder claim from acquisition, but `_emit_error_details` prints only flat allowlisted fields, so humans cannot see who holds the lease or when it expires. Retryable contention also lacks command-appropriate guidance.

## Decision

For text `already-claimed` errors from `acquire` and `acquire-bundle`, render a `HOLDER` block after the top-level `RESOURCE` when the error contains a claim mapping. Read only that redacted mapping and emit exactly `CLAIM_ID`, `AGENT_ID`, `WORK_KEY`, and `EXPIRES_AT`; do not reuse the full claim renderer or expose a token, checkpoint, session ID, owner ID, revision, guarantee, or bundle member list. If a synthetic or legacy error has no claim mapping, omit the holder block without failing.

Add exactly one final contention hint for both `already-claimed` and `resource-guarded`, scoped to acquire operations. Singleton `acquire` names its supported `--wait-timeout SECONDS` and optional `--poll-interval SECONDS`, plus selecting other ready work. `acquire-bundle` instead says to select other ready work or retry later because bundle acquisition does not support those wait options. Keep hint selection in text-only runtime rendering so JSON envelopes remain byte-for-byte unchanged. Other commands that happen to surface `resource-guarded` get no acquire-specific hint.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Text `already-claimed` output from singleton and bundle acquire prints `RESOURCE`, then a `HOLDER` block containing exactly `CLAIM_ID`, `AGENT_ID`, `WORK_KEY`, and `EXPIRES_AT` from the redacted error claim; it never prints a token, checkpoint, session ID, owner ID, revision, guarantee, or bundle member list from that claim. A missing or non-mapping claim omits `HOLDER` without crashing.
- [ ] #2 Text singleton `already-claimed` and `resource-guarded` output ends with exactly one `HINT` naming `--wait-timeout SECONDS`, optional `--poll-interval SECONDS`, and selecting other ready work.
- [ ] #3 Text bundle `already-claimed` and `resource-guarded` output ends with exactly one `HINT` suggesting other ready work or a later retry and does not name unsupported wait options; non-acquire `resource-guarded` output gets no acquire hint.
- [ ] #4 JSON error envelopes for both reasons and both acquire forms are byte-for-byte unchanged and contain no added hint field or newly exposed claim data.
- [ ] #5 `docs/cli-reference.md` documents the holder block and command-specific hint grammar, and CHANGELOG `Unreleased` records the text-output change.
- [ ] #6 Tests use fixed holder data to cover singleton and bundle contention, both retryable reasons, exact hint count and placement, missing and non-mapping claims, redaction, non-acquire scoping, and unchanged JSON; `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
