---
id: TASK-29
title: Add agent-facing CLI discovery guidance
status: Done
assignee:
  - '@brett'
created_date: '2026-07-15 19:07'
updated_date: '2026-07-15 19:53'
labels: []
dependencies: []
ordinal: 30000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Expose concise agent workflow discovery from the top-level CLI help without duplicating the normative Worklease skill. Point agents to the canonical project skill and explain when to use command help and schema-versioned JSON. Keep development builds on canonical documentation URLs and keep release version metadata separate from URL construction.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Top-level help points workflow-oriented agents to the canonical Worklease skill and project documentation.
- [x] #2 The guidance distinguishes workflow semantics from command syntax and directs automation to --json.
- [x] #3 Source and development builds do not construct a version-tag URL unless published release metadata is available.
- [x] #4 Subprocess coverage verifies the guidance is present and exits successfully without changing existing command behavior.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add concise top-level agent workflow guidance pointing to canonical project skill and docs. 2. Clarify command help versus schema-versioned --json automation and guard development/source documentation URLs from version-tag construction without published release metadata. 3. Add subprocess coverage for successful guidance and preserve existing help behavior.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in commit b1b41e7: top-level help now links the canonical worklease-workflow skill and README, distinguishes workflow semantics from command syntax, and directs automation to schema-versioned --json. Documentation URLs use main for source/development builds and only use a validated v<release> ref when WORKLEASE_PUBLISHED_RELEASE_VERSION is explicitly supplied. Added subprocess coverage for source, valid release metadata, invalid metadata fallback, and existing help behavior. Verification: targeted CLI tests (2 passed; full test_cli 39 passed), mise run lint passed, mise run format-check passed, mise run test passed (core and SDK), and mise run typecheck passed (core and SDK). Independent verifier PASS on all four acceptance criteria.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added agent workflow discovery guidance to top-level CLI help with canonical skill and project documentation links, command-help and schema-versioned JSON instructions, and safe release URL selection. Verified with subprocess coverage, full repository tests, lint, format-check, typecheck, and independent acceptance verification.
<!-- SECTION:FINAL_SUMMARY:END -->
