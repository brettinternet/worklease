---
id: TASK-85.3
title: Build the Go acceptance and safety test foundation
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.1
  - TASK-85.2
references:
  - tests
  - packages/worklease-source-sdk/tests
  - ../hum/integration
parent_task_id: TASK-85
priority: high
type: task
ordinal: 95000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The rewrite is free to change APIs and persistence, so byte-for-byte Python parity would constrain the design unnecessarily. Build a Go-native acceptance foundation that captures retained product capabilities and safety properties, while using selected Python tests only as evidence for edge cases worth preserving. Every later implementation task should be able to add focused fixtures without constructing its own harness.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Reusable test helpers provide isolated authorities, controllable wall and monotonic clocks, deterministic identifiers and credentials, subprocess execution, fixture repositories/worktrees, and permission assertions.
- [ ] #2 Black-box command tests capture exit status, stdout, stderr, JSON results, filesystem effects, and database effects without depending on unexported production internals.
- [ ] #3 Concurrency and crash-test helpers have bounded timeouts, deterministic cleanup, and diagnostics suitable for unattended CI and agent loops.
- [ ] #4 A retained-capability matrix links each product-contract capability to its owning task and eventual executable test, while intentionally removed Python surfaces are excluded.
- [ ] #5 The initial harness passes under normal and race-enabled Go tests on supported CI hosts.
<!-- AC:END -->
