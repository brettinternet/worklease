---
id: TASK-155
title: Shorten CI critical path and avoid duplicate release validation
status: To Do
assignee: []
created_date: '2026-10-21 01:56'
labels:
  - ci
  - testing
  - release
dependencies: []
references:
  - .github/workflows/ci.yml
  - .github/workflows/release.yml
  - mise.toml
  - 'https://github.com/brettinternet/worklease/actions/runs/39329277019'
  - 'https://github.com/brettinternet/worklease/actions/runs/39527899385'
priority: high
type: enhancement
ordinal: 99000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Publishing v1.9.3 launched CI runs 39527899383 and 39527899273 for the same commit via branch and tag pushes. Release run 39527899385 finished archive and container validation in about six minutes, then waited for the Linux quality job. Baseline run 39329277019 took about 24 minutes, dominated by 19m38s of sequential race tests. TASK-149 intentionally serialized gates and split CLI tests to prevent resource contention and package-deadline failures on small runners; simply restoring same-runner parallelism would regress that fix. The workflow also keys immutable Go build caches only by OS, architecture, and go.sum, excluding the Go toolchain version. Scope is workflow scheduling, cache identity, timing visibility, and exact-commit release gating; test-body optimization is separate.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Independent quality gates and race partitions can execute on separate runners with visible timings; every existing test and gate remains required, and partitions have demonstrably complete coverage without multiplying full-suite execution.
- [ ] #2 A branch and release-tag push for the same commit does not launch duplicate full CI suites; tag-only release attempts still obtain required validation, and publication fails closed unless the exact tagged commit passes all required gates.
- [ ] #3 Go build-cache identity distinguishes toolchain versions as well as platform and dependencies, and evidence demonstrates a new toolchain can populate a reusable cache.
- [ ] #4 Record comparable before/after CI critical-path and runner-minute measurements, showing reduced release wait without reintroducing resource-contention failures; verify both ordinary CI and the release-gating path.
<!-- AC:END -->
