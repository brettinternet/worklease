---
id: TASK-10
title: Add versioned resource policy plugins
status: To Do
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:33'
updated_date: '2026-07-14 03:33'
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Versioned lazy policy registry (AC1, AC2, AC4).** Replace the adapter fallback with a registry for built-ins plus Python entry points in `worklease.resource_policies`. Define contract version 1 descriptors/factories, explicit `generic` coordination-only policy, lazy loading, duplicate/version/descriptor validation, and deterministic no-fallback errors.
2. **[T2] Policy inspection and conformance kit (AC3-AC5).** Add `policy list` and `policy describe --name NAME` JSON/text commands and a reusable conformance module covering descriptor fields, collision avoidance, deterministic identities, worktree/common-dir behavior, process/session stability, supported policy upgrades, misspellings, and plugin failures.
3. **[T3] Packaging and extension documentation (AC1, AC6).** Add entry-point fixtures and wheel/editable installation tests. Document the policy/source-provider boundary, compatibility declaration, and that frozen standalone executables expose built-ins only and do not discover environment entry points.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now.

**Goal and target area:** Refactor existing `src/worklease/adapters` into a versioned resource-policy registry, extend `cli.py`, add conformance/tests, and declare a new entry-point group in `pyproject.toml`. Existing adapter paths and `ResourceKey` fields are the implementation pattern.

**Resolved decisions:** Use Python entry-point group `worklease.resource_policies`. Contract version 1 loads a descriptor/factory lazily by normalized exact name and reports origin distribution/version, contractVersion, keyPolicyVersion, scope, capability, genericExecutionGuarantee, and providerFencingSupported. Built-ins register without importing provider modules at core import. Unknown names return `resource-policy-not-found`; callers must request the built-in `generic` policy explicitly for coordination-only custom identities. Duplicate names, non-v1 contracts, malformed descriptors, import/factory failures, and unavailable extras fail deterministically with no fallback. Wheels/sdists/editable installs discover entry points; frozen executables expose built-ins only.

**Non-goals:** source/provider discovery, credentials, network calls, provider mutations, scheduling, plugin installation, or loading arbitrary filesystem modules.

**Evidence and assumptions:** Current `load_adapter` silently falls back to Linear and must be cut over cleanly. Current `ResourceKey` already carries capability/scope/fencing/guarantee fields. Python `importlib.metadata.entry_points` is sufficient; no runtime dependency is needed.

**Task/acceptance map:** T1→AC1/2/4; T2→AC3-5; T3→AC1/6.

**Pending verification:** import-boundary tests, installed wheel/editable entry-point fixture, misspelling/duplicate/version/load failures, cross-process/worktree identity, standalone built-ins-only behavior, full quality gates.

**Next action:** implement the v1 descriptor and registry while preserving built-in key outputs.

**Refinement checkpoint:** refined: TASK-10 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=f7c30463-000c-4401-8507-e810da4dc80d; claimRevision=3; refinement: complete.
<!-- SECTION:NOTES:END -->
