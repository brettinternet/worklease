---
id: TASK-51
title: Distinguish an invalid bearer token from a lost claim
status: Done
assignee:
  - '@codex-task-51'
created_date: '2026-09-07 03:28'
updated_date: '2026-09-07 08:28'
labels:
  - cli
  - api
dependencies: []
references:
  - src/worklease/store.py
priority: medium
type: enhancement
ordinal: 52000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ownership checks raise `stale-claim` when either the claim ID or the token mismatches. A truncated token file or mis-plumbed `--token-fd` therefore reports the same reason as genuine ownership loss, and the skill tells agents to stop on ownership loss. Propose a distinct reason (for example `invalid-token`) when the claim ID matches but the token does not, keep exit code 2, and add it to schemas, the README reason list, and the skill guidance. Adding a reason is a minor-version API change; document it in the compatibility notes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 heartbeat with the correct claim ID and a wrong token returns a distinct documented reason with exit 2 and no token material in output
- [x] #2 heartbeat with a wrong claim ID still returns stale-claim
- [x] #3 Schemas, README exit-code and reason documentation, and skills/worklease-workflow safe-failure guidance mention the new reason; tests cover both paths
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Update ownership validation so a matching claim ID with a mismatched token returns a sanitized invalid-token failure while wrong claim IDs remain stale-claim.
2. Extend schemas, README compatibility/reason documentation, and workflow safe-failure guidance for invalid-token.
3. Add focused tests for both ownership paths, run the full quality gates, review the diff, and finalize the task with evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented invalid-token across singleton, bundle, transfer, and release/replay ownership checks; wrong claim IDs remain stale-claim. Added CLI/schema regression coverage and updated README, changelog compatibility notes, and workflow safe-failure guidance. Quality gates passed: mise run lint, format-check, test (218 core + 19 SDK), and typecheck (core + SDK).

Adversarial review found and resolved two edge cases before integration: historical release replays with bad credentials remain stale-claim, and UTF-8 invalid credentials are compared as bytes rather than raising TypeError. Rebased onto current main after TASK-47/TASK-49/TASK-48 integration; post-rebase gates passed with 228 core and 19 SDK tests.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added the stable invalid-token reason for current claim IDs with wrong bearer credentials while preserving stale-claim for lost/historical claims. Updated schemas, README, changelog, and workflow guidance; added redaction, wrong-ID, UTF-8 credential, and replay regressions. Verified with focused CLI/store tests, independent acceptance verification, adversarial review, all project quality gates, and post-rebase full tests (228 core + 19 SDK).
<!-- SECTION:FINAL_SUMMARY:END -->
