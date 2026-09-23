---
id: TASK-128.5
title: Add the read-only GitHub Issues source adapter
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 21:58'
labels:
  - work-queue
  - github
  - reviewed
milestone: m-1
dependencies:
  - TASK-128.2
  - TASK-128.3
references:
  - skills/worklease-workflow/references/source-providers/github-issues.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: feature
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
D14 and plan sections 3, 10, and 14 fix how the queue reads GitHub. It sends HTTPS requests to GraphQL (and REST where needed) directly, with the credential from `gh auth token --hostname H --user U` for the configured account. It verifies the principal before use and serializes requests per host and account, as GitHub recommends. Use the TASK-126.5 probe results for exact query shapes and change-detection behavior.

This slice enumerates into memory. Persistent incremental sync is TASK-129.3, and the general scheduler is TASK-129.2; this task needs only the minimal per-account serialization that TASK-129.2 later replaces. Writes arrive in TASK-132.3. Register the adapter in the TASK-128.3 registry under `github`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The token comes from running `gh auth token --hostname <host> --user <account>` with argv only, and with GH_TOKEN, GITHUB_TOKEN, GH_ENTERPRISE_TOKEN, and GITHUB_ENTERPRISE_TOKEN removed from its environment. The token is held in memory only and never appears in files, logs, errors, or JSON, as shown by a test with a canary token
- [x] #2 Before any data request, the adapter verifies that the authenticated login equals the configured account. On a mismatch it returns an authentication diagnostic and shows no data
- [x] #3 Full enumeration uses GraphQL cursor pages of up to 100 in creation order, so ordinary edits do not move issues between pages. Results are deduplicated by node ID, pull requests are excluded, and totalCount is reported as the count observed in that response, not a multi-page snapshot
- [x] #4 blockedBy edges carry completeness derived from totalCount. Cross-repository references are source-qualified. Sub-issues are exposed as hierarchy and never as prerequisites
- [x] #5 Requests are serialized per host and account. retry-after, x-ratelimit-remaining/reset, and secondary rate limits are honored with backoff and jitter, and rate-limited is reported separately from offline
- [x] #6 401, 403 permission denied, SAML SSO required, 404, 410, rate limiting, and network failure map to distinct structured diagnostics. A 404 is reported as not found or inaccessible, never as deleted, because GitHub returns 404 on lost permission
- [x] #7 A repository rename or issue transfer (a redirect or a nameWithOwner mismatch) produces an identity diagnostic that marks claims unavailable (D24)
- [x] #8 Enterprise hosts use the configured host's API base. If dependency fields are missing, the capability is marked unsupported instead of failing the source
- [x] #9 Tests run against an httptest fake GitHub with recorded fixtures, and `go test` makes no live network calls
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Implement GitHub read adapter with verified gh credential, serialized HTTP GraphQL pagination, relationship evidence, identity and diagnostic handling. 2. Exercise fake HTTP and credential canary tests. 3. Run repository gates, review, commit and merge to main, record evidence and release claim.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented initial GitHub adapter and fake HTTP credential, pagination, relationship, diagnostic, and identity tests; focused go test ./internal/queue -run TestGitHub passes. Refining pagination completeness and rate handling before gates.

General review found six concrete issues: successful issue text misclassified as rate limit, scan dedup collision, redirect drift not latched, custom cross-repo source IDs, successful last-quota response lost, and GraphQL permission errors. Corrected all six and added focused regressions; no second general review planned.

Evidence: go test -race ./internal/queue -run TestGitHub -count=1 validates canary token env stripping, principal mismatch, cursor page dedup and observed totals, dependency completeness and cross-source hierarchy, serial requests, retry and offline, distinct HTTP/GraphQL diagnostics, redirect drift, enterprise API host, and no live network. mise run lint, format-check, test, typecheck, hooks passed on fe4fa0a; proposal D14/sections 3,10,14 unchanged by this implementation. Integration into main remains.

Post-commit validation on 701cb26: mise run lint, format-check, test, typecheck, hooks all passed. Integration deferred because main currently has another worker’s uncommitted edits to internal/queue/model.go (also touched by this branch); preserve unrelated edits. Next: after main is clean, merge task-128-5-github-adapter, rerun relevant gates, record merge commit and complete task; then verify worktree ownership before cleanup.

Fast-forward merged 701cb26 into main; post-integration mise run lint, format-check, test, typecheck, hooks all passed. Prior focused race tests and one general review recorded above. No remaining implementation blocker.

Review: fixed concurrent final-page scan panic, duplicate dependency edges proving completeness, stale binding after failed re-verification, unbounded abandoned scans (7643105, merged 7268f9a).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added read-only GitHub Issues queue adapter, verified with fake HTTP/canary and race tests and all repository gates; integrated commits fe4fa0a and 701cb26 into main.
<!-- SECTION:FINAL_SUMMARY:END -->
