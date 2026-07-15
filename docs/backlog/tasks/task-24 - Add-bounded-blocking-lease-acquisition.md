---
id: TASK-24
title: Add bounded blocking lease acquisition
status: In Progress
assignee:
  - '@codex-loop-fresh-20260714-worklease-pass'
created_date: '2026-07-15 03:22'
updated_date: '2026-07-15 04:00'
labels: []
dependencies: []
modified_files:
  - src/worklease/cli.py
  - tests/test_cli.py
  - README.md
priority: medium
type: enhancement
ordinal: 25000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Close the documented scarce-resource UX gap by adding bounded wait-and-acquire behavior to the singleton `acquire` command. A standalone read-only `wait` would create a time-of-check/time-of-use race, so the CLI must retry the existing atomic acquire operation instead. Existing callers remain immediate by default, lease guarantees remain same-host, and bundle members retain their bundle-only lifecycle.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A caller can request a bounded wait while acquiring one singleton resource, and success returns the atomically acquired claim rather than advisory availability.
- [x] #2 Waiting retries only transient singleton contention and succeeds when a free or expired singleton resource becomes acquirable.
- [x] #3 Timeout preserves the stable lease-conflict exit-code contract and returns redacted contention context without exposing a bearer token.
- [x] #4 Singleton acquisition does not wait or retry when the resource belongs to a bundle, including an expired bundle; the existing bundle-operation-required result is returned immediately.
- [x] #5 Calls that do not request waiting retain their current output, error, timing, and exit-code behavior.
- [x] #6 CLI documentation and deterministic tests cover success after release or expiry, heartbeat extension, timeout, transient resource guards, invalid wait options, and bundle-member rejection.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 DOD1 — Targeted deterministic wait-acquisition tests and all repository quality gates pass.
- [ ] #2 DOD2 — README syntax and compatibility claims match observed JSON/text CLI behavior, including redaction and exit code 2 on timeout.
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
### Implementation tasks
- [x] T1 — Add bounded singleton wait-and-acquire behavior, deterministic contract tests, and operator documentation (AC1-AC6).

### Target area
- Existing `src/worklease/cli.py`: add `acquire --wait-timeout SECONDS` and optional `--poll-interval SECONDS`; keep `LeaseStore.acquire` as the atomic ownership primitive.
- Existing `tests/test_cli.py`: cover retry decisions and JSON/text CLI compatibility with injected monotonic time and sleeping so tests do not use wall-clock delays.
- Existing `README.md`: document syntax, timeout behavior, retryable conflicts, bundle-member behavior, and the unchanged same-host guarantee.
- Existing response schemas remain applicable because the operation and envelopes do not change; update schema fixtures/tests only if implementation adds a response field, which is not planned.

### Resolved decisions and boundaries
- Do not add a standalone `wait` command: availability would be advisory and another caller could acquire before the waiter.
- `--wait-timeout` is optional and absent by default. Its presence enables waiting; zero performs one immediate attempt. Values must be finite and non-negative.
- `--poll-interval` defaults to 0.25 seconds, must be finite and greater than zero, and is valid only with `--wait-timeout`.
- Use a monotonic deadline and sleep for at most the lesser of the poll interval and remaining time. Re-run atomic `acquire` directly; never preflight with `status`.
- Retry only `already-claimed` and transient `resource-guarded`. Return every other `LeaseError` immediately.
- At deadline, return the last transient conflict unchanged with exit code 2 and existing redaction. Do not add a new exit code or expose tokens.
- A free or expired singleton row can be acquired by the existing store logic. If the resource is or becomes a bundle member, including an expired bundle, singleton acquire returns `bundle-operation-required` immediately and is not retried.
- Heartbeats may extend expiry, so reported expiry is never treated as a reservation or a guaranteed wake time.
- Non-goals: bundle waiting, unbounded waits, provider-side/cross-host fencing, status subscriptions, SQLite notification infrastructure, and library-level blocking APIs.

### Verification
- Deterministically exercise success after release, success after singleton expiry, heartbeat extension beyond the original expiry, timeout on `already-claimed`, timeout on repeated `resource-guarded`, immediate bundle rejection, invalid numeric options, unchanged no-wait behavior, token redaction, and JSON/text rendering.
- Run the repository quality gates required by `AGENTS.md` after implementation.

Implementation Notes:
--------------------------------------------------
Refinement checkpoint: refinement: complete.
Readiness: available; TASK-24 has no dependencies or external inputs.
Implementation snapshot: `README.md:165` already directs scarce-resource callers to wait for release or expiry, while `src/worklease/cli.py:197-212` exposes only immediate singleton acquisition. `src/worklease/store.py:1796-1976` provides the atomic acquire/reclaim primitive and returns `already-claimed` for active singleton contention; it checks bundle membership first and returns `bundle-operation-required` even for expired bundle members. `src/worklease/locking.py:27-40` makes `resource-guarded` a transient non-blocking file-lock conflict. `README.md:203-212` defines exit code 2 for lease/capability conflicts and stable reason values. Existing CLI tests in `tests/test_cli.py:17-109` provide subprocess JSON/text helpers and `tests/test_cli.py:697-723` verifies conflict token redaction.
Open questions: none. Least-confident assumption: a fixed 0.25-second local poll interval is responsive enough without jitter; it is reversible and explicitly configurable.
Next action: review TASK-24 after implementation commit 424ac3c53368ff1e9695ef6b66b59053814803f2.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement checkpoint: refinement: complete.
Readiness: available; TASK-24 has no dependencies or external inputs.
Implementation snapshot: `README.md:165` already directs scarce-resource callers to wait for release or expiry, while `src/worklease/cli.py:197-212` exposes only immediate singleton acquisition. `src/worklease/store.py:1796-1976` provides the atomic acquire/reclaim primitive and returns `already-claimed` for active singleton contention; it checks bundle membership first and returns `bundle-operation-required` even for expired bundle members. `src/worklease/locking.py:27-40` makes `resource-guarded` a transient non-blocking file-lock conflict. `README.md:203-212` defines exit code 2 for lease/capability conflicts and stable reason values. Existing CLI tests in `tests/test_cli.py:17-109` provide subprocess JSON/text helpers and `tests/test_cli.py:697-723` verifies conflict token redaction.
Open questions: none. Least-confident assumption: a fixed 0.25-second local poll interval is responsive enough without jitter; it is reversible and explicitly configurable.
Next action: implement T1 without changing `LeaseStore.acquire` semantics or adding a standalone wait operation.

T1 complete in commit 424ac3c53368ff1e9695ef6b66b59053814803f2. Added bounded singleton --wait-timeout/--poll-interval retrying only already-claimed/resource-guarded with monotonic deadlines; bundle members remain immediate bundle-operation-required. Added deterministic tests for release, expiry, heartbeat extension, timeout/redaction, invalid options, no-wait, and text/JSON behavior; documented operator syntax. Verification: targeted 7 tests, full CLI unittest (27 passed), mise run lint, mise run format-check, mise run typecheck, mise run test, mise run hooks. Next pass: REVIEW.
<!-- SECTION:NOTES:END -->
