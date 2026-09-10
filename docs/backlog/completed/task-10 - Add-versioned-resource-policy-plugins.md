---
id: TASK-10
title: Add versioned resource policy plugins
status: Done
assignee:
  - '@codex-loop-main'
created_date: '2026-07-14 02:33'
updated_date: '2026-07-14 06:46'
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
- [x] #1 Built-in and externally registered resource policies are discovered lazily through a documented, versioned extension contract without importing every plugin during core package import.
- [x] #2 Unknown provider names fail deterministically by default, while an explicit generic/custom path preserves caller-authorized coordination-only identities; tests cover a misspelled built-in provider.
- [x] #3 CLI commands list available policies and describe each policy's origin, contract version, key-policy version, claim scope, capability, generic execution guarantee, and provider-fencing support.
- [x] #4 Duplicate registrations, incompatible contract versions, load failures, malformed descriptors, and unavailable plugins produce stable schema-versioned errors without falling back to another policy.
- [x] #5 A reusable conformance suite proves resource collision avoidance and identity stability across processes, sessions, worktrees, and supported plugin upgrades.
- [x] #6 Documentation distinguishes resource-policy plugins from source workflow adapters and states which installation forms, including standalone executables, can load external plugins.
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

Implementation pass T1 claimed under claim 470CDAE4-7F02-47E6-A883-F05B1AA0A23A; canonical in-progress checkpoint.

Implementation checkpoint (T1 versioned lazy policy registry): commit fb6fee6ba17fac4e6d471c8844f369b06ea2d3c6 integrated into main. Added v1 ResourcePolicyDescriptor/Registration, lazy built-in and worklease.resource_policies entry-point loading, explicit generic policy, deterministic schemaVersion=1 no-fallback errors, duplicate/version/descriptor/load validation, and regression coverage. Verification: mise run lint, format-check, test (81 tests), typecheck, and hooks passed; CLI smoke: unknown provider exits 2 with resource-policy-not-found, generic provider exits 0. Next task: T2 policy inspection and conformance kit. Remaining acceptance: AC3-AC6 and review evidence.

Implementation pass T2 claimed under 4A9B55E7-8AB2-4DDB-9AD8-597DE0E5BA79; canonical in-progress checkpoint by @codex-loop-main; task T2 is the only task in this pass.

T1 verifier finding fixed: external policy factory LeaseError is now normalized to resource-policy-factory-failed with schemaVersion=1; regression test added. Review-fix commit cbca1b0a5ae1b5beed230d07a318d8159bef21b4 integrated into main. Focused adapter tests, full mise lint/format-check/test/typecheck, and hooks pass after fix. T1 remains complete; next task T2 policy inspection and conformance kit; review evidence remains for the accumulated item.

Implementation checkpoint (T2 policy inspection and conformance kit): commit 2fdc1172516fed9d27abbcb0629f0f9eeb2afb01 adds `policy list` and `policy describe --name NAME` JSON/text inspection with descriptor origin, contract/key-policy versions, scope, capability, generic execution guarantee, and provider-fencing support; exports a reusable conformance suite covering collision avoidance, repeated/process identity stability, equivalent-source stability, and policy-version checks. Verification: targeted adapter/CLI tests (25) passed; `mise run lint` passed; `mise run format-check` passed (25 files); `mise run test` passed (83 tests); `mise run typecheck` passed (0 errors); `mise run hooks` passed. Progress: T2 complete. Next task: T3 packaging and extension documentation. Remaining acceptance: AC6 and review evidence.

Integration checkpoint: implementation commit 2fdc1172516fed9d27abbcb0629f0f9eeb2afb01 cherry-picked into canonical main under the provider/repository transaction lock. Post-integration `mise run lint`, `mise run format-check`, `mise run test` (83 tests), and `mise run typecheck` all passed.

Implementation repair pass for TASK-10 T2 under verifier finding: make conformance collision checks honor source-scoped policies before isolation.

T2 verifier repair: commit 363b5a086d4a4618c2fdc39c06cdd774a52d63cc makes reusable conformance checks honor source-scoped policies, validate distinct source identities, and compare every sample item across subprocesses and equivalent sources; adds source-scope regression coverage. Post-repair verification: all five built-in policies conformance PASS; targeted adapter/CLI tests (26) passed; `mise run lint`, `mise run format-check`, `mise run test` (84 tests), `mise run typecheck`, and `mise run hooks` passed. T2 remains complete; next task T3 packaging and extension documentation; review evidence remains.

Integration checkpoint: verifier repair commit 363b5a086d4a4618c2fdc39c06cdd774a52d63cc was cherry-picked into canonical main as a97df18 under the provider/repository transaction lock. Post-integration full quality gates remain green (lint, format-check, 84 tests, typecheck); the repaired T2 conformance behavior passed all five built-in policy checks.

Final T2 verifier repair claim: extend source-scope collision checks across every sample item before isolation.

Final T2 verifier repair integration: source commit 1f5000351cd7153e643c7e96ef3cb45cfd587272 was cherry-picked into canonical main as 1fa1344 under the provider/repository transaction lock. Source-scoped collision avoidance now checks every sample item and regression coverage includes a per-item alternate-source collision. Post-integration verification: all five built-in policies conformance PASS; targeted adapter/CLI tests (27) passed; `mise run lint`, `mise run format-check`, `mise run test` (85 tests), and `mise run typecheck` passed; staged hooks passed on the repair commit. T2 remains complete; next task T3 packaging and extension documentation; review evidence remains.

Implementation checkpoint (T3 packaging and extension documentation): commit bb08734323a9b5d223d4483063a27af58d5afbe9. Added tests/fixtures/resource-policy-plugin entry-point package with wheel and editable install discovery tests; frozen runtime now suppresses external entry-point discovery and has regression coverage; updated README and source-provider references for the policy/provider boundary, explicit generic policy, contract version, and built-in-only standalone behavior. Verification: mise run lint, format-check, test (87 tests), typecheck, and staged mise run hooks passed. Progress: T3 complete. Next task: review accumulated item. Remaining acceptance: review evidence.

Integration checkpoint: implementation commit bb08734323a9b5d223d4483063a27af58d5afbe9 cherry-picked into canonical main as 1f30258124dda38e467125bcedf79a2da3ba748e under the provider/repository transaction lock. Unrelated provider task edits remained untouched. Next pass is review of the accumulated TASK-10 implementation.

Verifier review finding fixed in commit e123e58582fd49ed19929319b598b85d7d33c57d and integrated into canonical main as 9680dae. Corrected the policy inspection documentation to state that list/describe do not construct adapters; external registration loading remains explicit and truthful. Focused adapters/plugin tests, lint, typecheck, and staged hooks passed after the fix. Independent verifier otherwise PASS for fixture, wheel/editable discovery, frozen built-ins-only behavior; review finding resolved.

Implementation pass T3 claimed under claim FBD62F6F-2439-45DA-A610-C52EAC7FCD12; canonical in-progress checkpoint.

Correction: all refined implementation tasks are complete; this claim made no code changes and is released for the required accumulated-item review pass.

REVIEWED: accumulated implementation commits fb6fee6, cbca1b0, 3c738ba, a97df18, 1fa1344, 1f30258, and 9680dae. Full implementation review completed: no actionable findings. Verification: mise run lint, format-check, test (92 tests), typecheck, and hooks passed; CLI smoke covered policy list, policy describe --name generic, and unknown policy failure (exit 2). Review depth: full rubric for plugin loading, resource identity/collision, CLI contract, packaging, error/redaction, frozen runtime, and documentation.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented versioned lazy resource-policy plugins, explicit generic coordination policy, deterministic inspection commands, reusable conformance checks, packaging fixtures, and source-provider boundary documentation. Reviewed the accumulated implementation at commits fb6fee6, cbca1b0, 3c738ba, a97df18, 1fa1344, 1f30258, and 9680dae with no actionable findings. Verified with mise run lint, format-check, test (92 tests), typecheck, hooks, and policy CLI smoke checks.
<!-- SECTION:FINAL_SUMMARY:END -->
