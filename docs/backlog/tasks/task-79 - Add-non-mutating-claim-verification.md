---
id: TASK-79
title: Add non-mutating claim verification
status: To Do
assignee: []
created_date: '2026-09-12 03:06'
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
