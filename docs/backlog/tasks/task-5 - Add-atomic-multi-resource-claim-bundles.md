---
id: TASK-5
title: Add atomic multi-resource claim bundles
status: To Do
assignee: []
created_date: '2026-07-14 02:06'
labels:
  - coordination
  - leases
dependencies: []
references:
  - src/worklease/models.py
  - src/worklease/store.py
  - src/worklease/locking.py
  - 'https://github.com/simke9445/agentlocks'
  - 'https://github.com/ThatHunky/agent-coord'
priority: high
type: feature
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Allow a caller to acquire a finite set of exact opaque resources as one all-or-nothing claim bundle. This supports work that spans multiple files, source records, or related provider-local resources without partial ownership or lock-order deadlocks. Resource identity remains caller-supplied and opaque; glob expansion and provider discovery stay outside the core.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A bundle acquisition either owns every requested resource or owns none of them, including when one resource is already active or acquisition fails partway through.
- [ ] #2 Bundle acquisition uses deterministic ordering and same-host coordination so concurrent overlapping bundles cannot deadlock or leave partial claims behind.
- [ ] #3 The receipt provides a coherent way to inspect, heartbeat, execute guarded work against, and release the bundle; stale, expired, conflicting, and repeated requests have stable JSON errors and idempotent behavior.
- [ ] #4 Exact duplicate resources, empty bundles, oversized bundles, and invalid resource identities are rejected deterministically without leaking bearer tokens.
- [ ] #5 Automated concurrency tests and README/workflow documentation cover bundle semantics, conflict reporting, lifecycle behavior, and the unchanged provider-neutral guarantee.
<!-- AC:END -->
