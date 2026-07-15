---
id: TASK-29
title: Add agent-facing CLI discovery guidance
status: In Progress
assignee:
  - '@brett'
created_date: '2026-07-15 19:07'
updated_date: '2026-07-15 19:45'
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
- [ ] #1 Top-level help points workflow-oriented agents to the canonical Worklease skill and project documentation.
- [ ] #2 The guidance distinguishes workflow semantics from command syntax and directs automation to --json.
- [ ] #3 Source and development builds do not construct a version-tag URL unless published release metadata is available.
- [ ] #4 Subprocess coverage verifies the guidance is present and exits successfully without changing existing command behavior.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add concise top-level agent workflow guidance pointing to canonical project skill and docs. 2. Clarify command help versus schema-versioned --json automation and guard development/source documentation URLs from version-tag construction without published release metadata. 3. Add subprocess coverage for successful guidance and preserve existing help behavior.
<!-- SECTION:PLAN:END -->
