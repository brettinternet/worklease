---
id: TASK-94
title: Close MCP projection redaction gaps found in boundary review
status: Done
assignee:
  - '@codex'
created_date: '2026-09-12 22:40'
updated_date: '2026-09-12 22:45'
labels:
  - go-rewrite
dependencies: []
priority: high
type: bug
ordinal: 119000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Follow-up review of completed TASK-85 and TASK-88: output.Redact skips typed structs and slices, allowing bearer-shaped metadata to pass through MCP status, list, events, and resource arrays. Review remaining security and concurrency boundaries, fix bounded defects, and track larger changes separately.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 MCP text and structured output redact credential-shaped metadata in typed projections and slices while preserving public hashes and exact numeric values
- [x] #2 Focused regression tests and repository quality gates pass; review findings and deferred work are recorded
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce typed projection and slice redaction escapes through MCP. 2. Normalize MCP success projections into JSON values before redaction and add regressions. 3. Complete boundary review and file larger fixes. 4. Run quality gates and hooks, record evidence, and commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Both new regressions failed before the fix and pass after JSON normalization. TestMCPRedactsTypedPublicMetadata directly serializes acquire/status/list/events/verify output; TestMCPProjectionPreservesExactNumbersAndPublicHashes covers success and error typed private fields, 64-hex public hashes, and integers beyond 2^53. mise run lint, format-check, test, and typecheck passed using writable temporary caches. Targeted race tests passed for MCP/handle/GC/watch and independent lease/store/ledger review. Reproduced and filed TASK-95 and TASK-96; broader CLI policy remains TASK-90. Full findings: docs/reviews/go-rewrite-boundaries-follow-up.md.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Closed MCP typed projection and slice redaction bypasses without losing exact numbers or public hashes. Verified pre-fix failures, passing regressions, repository quality gates, and targeted race tests. Recorded the security/concurrency review and larger handle-path/replay findings in TASK-95 and TASK-96.
<!-- SECTION:FINAL_SUMMARY:END -->
