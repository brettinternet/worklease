---
id: TASK-53
title: Detect wall-clock regressions in lease expiry
status: done
type: bug
priority: low
assignees: ['@brett']
labels: [store]
rank: a601cbe6d9601cb92
refs: [src/worklease/store.py]
files: [README.md, src/worklease/acquisition.py, src/worklease/claims.py, src/worklease/lifecycle.py, src/worklease/models.py, src/worklease/operations.py, src/worklease/projections.py, src/worklease/reconciliation.py, tests/test_store.py]
created: 2026-09-07T03:28:00Z
updated: 2026-09-07T14:13:00Z
---

Lease expiry is compared against `time.time()` with no sanity bound. A backward clock step keeps a short lease active far past its TTL; a forward step expires a live lease and lets another owner reclaim it while the first owner still believes it holds the lease. `acquire_ttl` is already persisted, so a claim can be treated as expired when the remaining time exceeds acquire_ttl (backward step) without a schema change. Decide whether the forward-step case needs anything beyond documentation.

## Acceptance criteria

- [x] A backward wall-clock step larger than the TTL causes the claim to report expired and heartbeat to fail claim-expired; covered by an injected-clock test
- [x] README documents the wall-clock dependency and the mitigation

## Plan

1. Add one shared lease-active predicate that detects apparent remaining lifetime beyond the current persisted renewal lifetime.
2. Route singleton and bundle status, acquisition, lifecycle, operation, and reconciliation expiry decisions through the predicate.
3. Cover acquisition and shortened-renewal clock regressions with injected-clock tests and document backward/forward wall-clock behavior.
4. Run repository quality gates and adversarial review, then integrate commit 4c41853.

## Notes

Implemented a shared lease-active predicate based on persisted acquire_ttl, applied it to status and all lease-current decisions, added injected backward-clock coverage, and documented backward/forward wall-clock behavior. Focused regression test passes.

Validation passed: focused injected-clock tests; mise run lint; mise run format-check; mise run test (228 core and 19 SDK tests); mise run typecheck; mise run hooks. Reviewer found one shortened-renewal defect, which was fixed and re-reviewed PASS.

Final implementation derives the current TTL from persisted heartbeat_at/expires_at so TTL-changing renewals are bounded correctly; acquire_ttl remains unchanged for acquire retry semantics.

Superseded by TASK-56. AC #1 as written ('a backward wall-clock step larger than the TTL causes the claim to report expired') encoded the wrong remedy: the shipped predicate reduced to 'now >= heartbeat_at', so any backward step past the last renewal expired the lease, and because the same predicate gated the contention paths a 50ms step let a second agent acquire a resource whose holder was still working. That is the exact double-ownership hazard this task's own description flagged for forward steps. The unbounded over-hold this task set out to fix is still fixed, now by re-anchoring an implausible expiry at the next contention rather than by expiring a live holder.

## Summary

Centralized lease-active checks so backward wall-clock regressions expire singleton and bundle claims consistently, including after TTL-changing renewals. Documented wall-clock limits and forward-jump mitigation. Verified with injected-clock tests, the full 247-test suite, lint, formatting, type checks, hooks, and adversarial review; implementation commit 4c41853.
