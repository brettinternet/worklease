---
id: TASK-11
title: Publish source provider extension SDK
status: In Progress
assignee:
  - '@codex-loop-main'
created_date: '2026-07-14 02:33'
updated_date: '2026-07-14 14:30'
labels:
  - providers
  - sdk
  - plugins
dependencies:
  - TASK-10
references:
  - skills/worklease-source-workflow/references/provider-contract.md
priority: medium
type: enhancement
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Turn the documented source-provider capability contract into a stable typed SDK and conformance kit for external provider packages. Provider SDKs, credentials, network calls, scheduling, and authoritative mutations remain outside the lease core.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The public SDK defines typed source, source-qualified work reference, work item, provider receipt, capability result, and source-provider protocol contracts matching the documented provider boundary.
- [ ] #2 The contract covers source resolution, complete discovery, authoritative reads, resource-policy selection, authorized state/progress writes, review boundaries, archive behavior, provider versions, and durable receipts without defining a scheduler or claim lifecycle.
- [ ] #3 A reusable conformance kit verifies source qualification, dependency closure, unsupported-capability behavior, stale provider-version rejection, ambiguous outcomes, receipt durability, token redaction, and truthful provider-fencing declarations.
- [ ] #4 An example external provider package composes the SDK with a TASK-10 resource policy without importing provider dependencies into worklease core modules.
- [ ] #5 Versioning and compatibility documentation defines how third-party providers declare supported SDK and resource-policy contract versions.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Standalone typed provider SDK (AC1, AC2, AC5).** Create a separate stdlib-only `worklease-source-sdk` distribution under `packages/worklease-source-sdk` with typed immutable Source, source-qualified WorkRef, WorkItem, ProviderReceipt, CapabilityResult, and SourceProvider protocol matching the existing provider contract. Include `py.typed`, contract version 1, and no lease-core/provider runtime dependency.
2. **[T2] Provider conformance kit (AC2, AC3).** Add reusable tests/helpers for ordered source resolution, complete dependency closure, authoritative version reads, resource-policy selection, supported/unsupported mutations, stale-version rejection, ambiguous outcomes, durable receipt shape, redaction, and truthful fencing declarations. The kit validates adapters; it does not schedule or manage claims.
3. **[T3] External composition example and compatibility guide (AC4, AC5).** Add a test-only example provider distribution that depends on the SDK and registers a TASK-10 resource policy entry point without adding provider imports to `worklease`. Document SDK/resource-policy contract versions, compatibility rules, credentials/authority ownership, release/build commands, and extension boundaries.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** ready after TASK-10. TASK-10 is refined and specifies the version-1 resource-policy entry-point contract, but remains incomplete; this item starts after that contract is implemented and available to the example provider.

**Goal and target area:** Use the existing contract at `skills/worklease-source-workflow/references/provider-contract.md` as the normative input. Create `packages/worklease-source-sdk/pyproject.toml`, `packages/worklease-source-sdk/src/worklease_source_sdk/`, conformance tests, an example provider fixture, and compatibility documentation. These are item-local new paths; no missing reference remains.

**Resolved decisions:** Publish a separate distribution/import package named `worklease-source-sdk` / `worklease_source_sdk`, Python >=3.14, stdlib-only, typed with `py.typed`, contract version 1. Models use source-qualified opaque IDs and immutable dataclasses/protocols. ProviderReceipt identifies operation, source/item, provider version, durable receipt/version locator, and ambiguous/confirmed state without secret payloads. CapabilityResult explicitly represents supported/unsupported operations. SourceProvider covers resolution, complete discovery, authoritative reads, resource-policy selection, authorized state/progress writes, explicit review boundaries, archive, and provider versions/receipts; scheduling and claim lifecycle stay outside.

**Non-goals:** bundled provider SDKs, credentials, network clients, authoritative mutations in lease core, graph scheduling, lease acquisition/heartbeat/release, or provider-side fencing claims without evidence.

**Evidence and assumptions:** The existing provider contract already separates policy selection, provider receipts, authority, dependency closure, and fencing truth. TASK-10 supplies only the resource-policy plugin interface consumed by the example; it does not own source discovery or mutations.

**Task/acceptance map:** T1→AC1/2/5; T2→AC2/3; T3→AC4/5.

**Pending verification:** isolated SDK wheel/sdist/type smoke tests, conformance self-tests against good/failing fixtures, dependency closure/stale-version/ambiguity/redaction cases, external entry-point composition after TASK-10, root quality gates.

**Next action:** after TASK-10 completes, implement T1 directly from the provider contract before adding fixtures.

**Refinement checkpoint:** refined: TASK-11 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=2b1b4ee5-1c42-4d33-ab3c-0957ce7c1035; claimRevision=3; prerequisite=TASK-10; refinement: complete.

Implementation checkpoint (T1 standalone typed provider SDK): commit 4674433690657adc3fa531c59c2a83b0760801de. Added packages/worklease-source-sdk as a stdlib-only typed distribution with immutable Source, WorkRef, WorkItem, ProviderReceipt (confirmed/ambiguous outcome), CapabilityResult, ResourcePolicySelection, ReviewBoundary, and SourceProvider protocol; included py.typed, contract version 1, package and SDK-local Pyright configs, and contract smoke tests. Verification: mise run lint, format-check, test, typecheck passed; SDK unittest (4 tests), SDK standalone pyright, wheel/sdist build passed. Next task: T2 provider conformance kit. Remaining criteria: AC2-AC5.

Integration checkpoint: T1 source commit 4674433690657adc3fa531c59c2a83b0760801de cherry-picked into canonical main as 3c66b77 under the provider/repository transaction lock. Canonical post-integration mise run lint, format-check, test, and typecheck passed.

Implementation pass T2 claimed under claim 25DA9323-BAB5-4C48-98DE-4F3C8860D25B; worker-attempt 0DEA0118-01CA-4347-BEB0-7849BF34A102; canonical in-progress checkpoint refreshed.

Implementation checkpoint (T2 provider conformance kit): commit f6669a7b3971fe7f7f3938811f3817bdeab917a7. Added reusable provider conformance case/report/helpers covering source qualification, dependency closure, unsupported capabilities, stale-version rejection, ambiguous receipts, receipt durability/redaction, and truthful fencing declarations; added 10 SDK conformance tests and public exports. Verification: SDK unittest (10 tests) passed; mise run lint, format-check, test, typecheck, hooks passed; uv build --wheel --sdist passed and artifacts include conformance.py. Next task: T3 external composition example and compatibility guide. Remaining criteria: AC4-AC5.

Integration checkpoint: T2 implementation commit f6669a7b3971fe7f7f3938811f3817bdeab917a7 cherry-picked into canonical main as 37906f22ad1dcc97407ca6d50afd20ec91c64af0 under the provider/repository transaction lock. Canonical post-integration mise run lint, format-check, test, and typecheck passed; canonical SDK conformance unittest (10 tests) passed.
<!-- SECTION:NOTES:END -->
