---
id: TASK-111
title: Unify no-claim errors and checkpoint input guidance
status: To Do
assignee: []
created_date: '2026-09-16 04:35'
labels:
  - ergonomics
dependencies: []
priority: medium
type: bug
ordinal: 153000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Bare contextual commands fail inconsistently when no claim exists: `status` and `verify` report 'selected handle is unavailable' while `heartbeat`, `release`, and `checkpoint` report 'handle is missing'. Neither tells a first-time user that the fix is `worklease acquire --path FILE`. Separately, bare `checkpoint` on a live claim reports 'checkpoint must be canonical JSON no larger than 8 KiB' when the real problem is that neither `--data` nor `--data-file` was supplied. Both violate the CLI principle that a command missing a required input names that input and shows an example instead of leaking an internal validation message.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 With no contextual claim, `status`, `verify`, `heartbeat`, `checkpoint`, and `release` return the same reason code and one-line message that names the missing claim and shows `worklease acquire --path FILE` as the next command; `--json` envelopes carry the same reason.
- [ ] #2 `checkpoint` without `--data` or `--data-file` fails with an invalid-argument message naming both options and an example; the JSON size/canonical message is reserved for supplied but invalid payloads.
- [ ] #3 Tests cover each command's bare no-claim path and the bare `checkpoint` path; `docs/cli-reference.md` error table reflects the unified message.
<!-- AC:END -->
