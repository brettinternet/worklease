---
id: TASK-143.4
title: Declare external adapter protocol v1 stable for outside authors
status: To Do
assignee: []
created_date: '2026-09-25 16:38'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies: []
documentation:
  - docs/external-adapter-protocol.md
  - docs/external-adapter-protocol.schema.json
parent_task_id: TASK-143
priority: medium
type: feature
ordinal: 76000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The protocol document is written as the internal S7 wire contract. Outside authors need to know whether v1 is stable to build on, where the canonical schema lives, and how compatibility and deprecation work before they invest in an adapter.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The protocol document states v1's stability status, what may change compatibly within v1, and the deprecation process, referencing the existing compatibility policy rather than duplicating it.
- [ ] #2 The JSON schema is published at a versioned, documented path with a stable `$id`, and the doc-test verifies the path and `$id` match.
- [ ] #3 The manifest's protocol range and the host's supported majors are reported by a `--json` CLI command so tooling can check compatibility without starting an adapter.
- [ ] #4 The authoring guide links the stable spec and schema as the entry point for new authors.
<!-- AC:END -->
