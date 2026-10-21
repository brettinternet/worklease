---
id: TASK-154
title: Reduce subprocess-heavy race test runtime without losing coverage
status: To Do
assignee: []
created_date: '2026-10-21 01:56'
labels:
  - testing
  - ci
dependencies: []
references:
  - 'https://github.com/brettinternet/worklease/actions/runs/39329277019'
  - internal/testkit
  - internal/cli
  - internal/queue
  - docs/backlog/tasks/task-148
priority: high
type: enhancement
ordinal: 98000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The v1.9.3 release exposed a longstanding CI bottleneck. Successful CI run 39329277019 spent 1178.28s in race tests out of a roughly 24-minute Linux quality job; normal tests took 122.38s. Race package timings were 487.449s for CLI TestQueue cases, 285.569s for internal/queue, and 118.240s for other CLI cases. TASK-148 previously reduced real-provider duplication but did not resolve this race overhead. Re-executed test binaries and the race runtime exit delay are hypotheses to measure, not established root causes. Preserve provider safety checks, process cleanup, and race detection; do not obtain speed by skipping coverage or widening timeouts.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Record repeatable per-test and package race timings with provider tests enabled, distinguishing test execution, subprocess overhead, and build time; identify measured dominant causes.
- [ ] #2 Reduce the measured dominant test overhead and record before/after timings on equivalent runner resources, including total CPU or runner time rather than only sharded wall time.
- [ ] #3 All existing behavioral and provider coverage remains exercised; changed process helpers retain bounded cleanup and surface race failures, verified by focused repeated race tests and a successful full CI run.
<!-- AC:END -->
