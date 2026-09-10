---
id: TASK-43
title: Generate lifecycle identifiers and read agent identity from the environment
status: Done
assignee:
  - '@brett'
created_date: '2026-09-07 03:26'
updated_date: '2026-09-07 14:13'
labels:
  - cli
  - devex
dependencies: []
references:
  - README.md
  - src/worklease/cli.py
modified_files:
  - README.md
  - docs/claim-model.md
  - skills/worklease-workflow/SKILL.md
  - skills/worklease-workflow/references/contract.md
  - src/worklease/cli.py
  - src/worklease/schemas/v1/commands.json
  - src/worklease/schemas/v1/common.json
  - tests/test_cli.py
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
- [x] #1 acquire succeeds with only --resource when WORKLEASE_AGENT_ID is set, and its output includes the generated claimId, sessionId, ownerId, and workKey
- [x] #2 Omitting --agent-id without WORKLEASE_AGENT_ID fails with exit 64 and a HINT naming the env var and flag
- [x] #3 heartbeat, checkpoint, exec, release, replace-file, reconcile-*, transfer and bundle equivalents accept an omitted --operation-id and echo the generated OPERATION_ID
- [x] #4 Caller-supplied identifiers behave exactly as before, including idempotent replay and operation-id-request-mismatch
- [x] #5 JSON schemas, README lifecycle examples, help epilogs, and skills/worklease-workflow reflect the new defaults; tests cover generated and explicit paths
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Trace lifecycle and bundle CLI identifier parsing, outputs, schemas, help, and documentation.
2. Add centralized defaults for generated IDs, environment-derived agent identity, and resource-derived work keys while preserving explicit replay semantics.
3. Add generated and explicit-path tests across lifecycle, transfer, and bundle commands; update schemas, README, help epilogs, and workflow skill.
4. Run focused and full quality gates, review the diff, merge to main, and clean up the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented centralized CLI lifecycle defaults using cryptographically random 128-bit identifiers, WORKLEASE_AGENT_ID fallback validation, resource-derived work keys, and expanded text claim fields. Preserved caller-supplied values unchanged through dispatch.

Validation: focused generated/default-path CLI tests passed (5/5); mise run lint, format-check, test (207 core + 19 SDK), and typecheck passed. Independent verifier passed acceptance criteria 1-5. Adversarial review found one misleading transfer hint, which was corrected and covered by a regression assertion.

Post-delivery review (TASK-56) found and fixed defects in this work; see TASK-56 for the specific defect, the fix, and its regression test.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added generated lifecycle and mutation identifiers, environment-derived agent identity, resource-derived work keys, and complete text/JSON visibility across singleton, bundle, and transfer CLI paths. Updated help, schemas, README, claim-model documentation, and the workflow skill. Verified with focused CLI coverage, 226 passing tests, lint, formatting, type checks, independent acceptance verification, and adversarial review.
<!-- SECTION:FINAL_SUMMARY:END -->
