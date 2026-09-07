---
id: TASK-43
title: Generate lifecycle identifiers and read agent identity from the environment
status: To Do
assignee: []
created_date: '2026-09-07 03:26'
labels:
  - cli
  - devex
dependencies: []
references:
  - README.md
  - src/worklease/cli.py
priority: high
type: enhancement
ordinal: 44000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Every `acquire` currently requires the caller to invent five identifiers (`--claim-id`, `--agent-id`, `--session-id`, `--owner-id`, `--work-key`) and every mutation requires a fresh `--operation-id`. The README already prescribes generating fresh claim/session/owner/operation IDs per attempt, so the CLI should do that by default. Humans and agents should be able to run `worklease acquire --resource R` with nothing else.

Scope: default `--claim-id`, `--session-id`, `--owner-id`, and `--operation-id` to fresh random IDs when omitted; default `--agent-id` from `WORKLEASE_AGENT_ID` and fail with an actionable hint when neither is available; default `--work-key` to the resource. Generated IDs must be echoed in text and JSON output so callers can replay or transfer. Caller-supplied IDs keep exact current idempotency semantics. Apply the same defaults to bundle and transfer successor arguments.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 acquire succeeds with only --resource when WORKLEASE_AGENT_ID is set, and its output includes the generated claimId, sessionId, ownerId, and workKey
- [ ] #2 Omitting --agent-id without WORKLEASE_AGENT_ID fails with exit 64 and a HINT naming the env var and flag
- [ ] #3 heartbeat, checkpoint, exec, release, replace-file, reconcile-*, transfer and bundle equivalents accept an omitted --operation-id and echo the generated OPERATION_ID
- [ ] #4 Caller-supplied identifiers behave exactly as before, including idempotent replay and operation-id-request-mismatch
- [ ] #5 JSON schemas, README lifecycle examples, help epilogs, and skills/worklease-workflow reflect the new defaults; tests cover generated and explicit paths
<!-- AC:END -->
