---
id: TASK-79
title: Add non-mutating claim verification
status: Done
assignee: []
created_date: '2026-09-12 03:06'
updated_date: '2026-09-12 04:45'
labels: []
dependencies: []
references:
  - 'https://github.com/ssheleg/agent-sync'
  - 'https://github.com/dicklesworthstone/mcp_agent_mail'
  - docs/claim-model.md
priority: high
type: feature
ordinal: 86000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Coding agents commonly mutate repositories through native edit and shell tools rather than through `worklease exec` or `replace-file`. A caller needs a cheap, non-mutating way to prove that its private lease handle still possesses the current unexpired ownership epoch immediately before such a mutation, while preserving Worklease’s truthful coordination-only boundary.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A CLI command and supported public API verify a private lease handle against the current claim, including claim identity, bearer credential, revision, expiry, and expected singleton or ordered bundle resources.
- [ ] #2 Successful verification does not change the claim revision, expiry, checkpoint, operation history, or retained lifecycle history.
- [ ] #3 Missing, expired, stale, mismatched, and invalid-credential handles fail with stable machine-readable reasons and actionable text output.
- [ ] #4 Verification output and failures never expose bearer tokens, token hashes, checkpoint bodies, or lease-file contents.
- [ ] #5 Documentation states that verification is a cooperative precondition check, not an atomic fence around a later direct edit or provider mutation.
- [ ] #6 Tests cover singleton and bundle success, every failure class, redaction, and the absence of state mutation.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a dedicated read-only verification request/result API that reuses current-claim validation without exposing secrets or mutating state.
2. Add one handle-driven verify CLI command for singleton and ordered bundle handles, including contextual defaults, stable diagnostics, text output, and schemas.
3. Document verification as a cooperative precondition rather than an atomic fence.
4. Add store, CLI, schema, redaction, and non-mutation tests; run review and all quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Closed 2026-09-12 as superseded before starting the unattended go-rewrite loop, so readiness-based selection cannot pick Python-era work. Acceptance criteria intentionally left unchecked: they were not delivered here. Delivery is owned by TASK-85.12.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Superseded by TASK-85.12 in the Go rewrite (TASK-85). Not delivered in Python. Go Product Contract (docs/backlog/docs/go-rewrite/doc-2) section 10.3 (read-only verify with cause codes and the Claude Code hook mode) fixes the behavior. A Python implementation was started by @pi-loop and stopped before any code was committed.
<!-- SECTION:FINAL_SUMMARY:END -->
