---
id: TASK-10
title: Add versioned resource policy plugins
status: To Do
assignee: []
created_date: '2026-07-14 02:33'
labels:
  - adapters
  - plugins
  - coordination
dependencies: []
references:
  - src/worklease/adapters
  - pyproject.toml
priority: high
type: feature
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Allow installed packages to contribute deterministic resource-key policies without adding provider discovery, network clients, or provider writes to the lease core. Replace silent unknown-provider fallback with an explicit generic coordination policy so misspellings cannot create unintended ownership domains.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Built-in and externally registered resource policies are discovered lazily through a documented, versioned extension contract without importing every plugin during core package import.
- [ ] #2 Unknown provider names fail deterministically by default, while an explicit generic/custom path preserves caller-authorized coordination-only identities; tests cover a misspelled built-in provider.
- [ ] #3 CLI commands list available policies and describe each policy's origin, contract version, key-policy version, claim scope, capability, generic execution guarantee, and provider-fencing support.
- [ ] #4 Duplicate registrations, incompatible contract versions, load failures, malformed descriptors, and unavailable plugins produce stable schema-versioned errors without falling back to another policy.
- [ ] #5 A reusable conformance suite proves resource collision avoidance and identity stability across processes, sessions, worktrees, and supported plugin upgrades.
- [ ] #6 Documentation distinguishes resource-policy plugins from source workflow adapters and states which installation forms, including standalone executables, can load external plugins.
<!-- AC:END -->
