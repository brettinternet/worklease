---
id: TASK-53
title: Detect wall-clock regressions in lease expiry
status: To Do
assignee: []
created_date: '2026-09-07 03:28'
labels:
  - store
dependencies: []
references:
  - src/worklease/store.py
priority: low
type: bug
ordinal: 54000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Lease expiry is compared against `time.time()` with no sanity bound. A backward clock step keeps a short lease active far past its TTL; a forward step expires a live lease and lets another owner reclaim it while the first owner still believes it holds the lease. `acquire_ttl` is already persisted, so a claim can be treated as expired when the remaining time exceeds acquire_ttl (backward step) without a schema change. Decide whether the forward-step case needs anything beyond documentation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A backward wall-clock step larger than the TTL causes the claim to report expired and heartbeat to fail claim-expired; covered by an injected-clock test
- [ ] #2 README documents the wall-clock dependency and the mitigation
<!-- AC:END -->
