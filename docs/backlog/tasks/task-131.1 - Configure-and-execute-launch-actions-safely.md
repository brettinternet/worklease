---
id: TASK-131.1
title: Configure and execute launch actions safely
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 14:53'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies:
  - TASK-130
references:
  - internal/config/profile.go
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-131
priority: high
type: feature
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Launch actions live under `launch:` in queue.yaml (D10, plan section 12). Each has a name, an argv array, an optional cwd, and an optional passEnv list. Placeholders are limited to adapter-validated identifiers (`{ref}`, `{sourceId}`, `{itemId}`, `{checkout}`), and each substitutes into exactly one argv element. No shell runs.

The child environment is built from scratch. It holds the fixed allowlist (PATH, HOME, USER, LOGNAME, SHELL, TERM, LANG, LC_*, TMPDIR, XDG_*_HOME), the variables named in passEnv, and WORKLEASE_PROFILE, WORKLEASE_QUEUE_AUTHORITY_ID, WORKLEASE_QUEUE_REF, and WORKLEASE_QUEUE_RESOURCES (a JSON array). It never includes titles, bodies, or a session ID, because the worker generates its own session. Titles and bodies are attacker-controllable (plan section 10).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 queue.yaml accepts `launch:` entries with a unique name, a non-empty argv, an optional cwd, and an optional passEnv list. Unknown placeholders and unknown keys are rejected with path-specific errors
- [x] #2 Each placeholder is replaced within exactly one argv element with an adapter-validated identifier. The process starts with exec semantics and never through a shell
- [x] #3 An action whose placeholders cannot be resolved for the item (for example `{checkout}` on an API-only source without an explicit cwd) is disabled with reason `unresolved-placeholder`
- [x] #4 docs/queue.md warns that argv substitution prevents shell expansion but not target-program option parsing, and shows explicit value arguments or `--`. A test passes an identifier beginning with `-` and shows it is never parsed as an option by a sample launcher that uses `--`
- [x] #5 The child environment contains only the allowlist, the passEnv variables, and the four WORKLEASE_* variables. A test with GH_TOKEN, GITHUB_TOKEN, and a canary secret in the parent environment proves none reach the child unless passEnv names them
- [x] #6 WORKLEASE_PROFILE and WORKLEASE_QUEUE_RESOURCES carry the view's authority profile and the item's exact resources (matching the TASK-126.4 vectors)
- [x] #7 Titles, bodies, and session IDs never appear in argv or the environment. A test with a hostile title and hostile IDs (containing spaces, quotes, `$()`, newlines, and leading `-`) shows no injection
- [x] #8 docs/queue.md documents launch actions, the environment contract, and the trust model
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend strict owner-private queue config with validated launch argv/cwd/passEnv and placeholder syntax. 2. Add a queue launch handoff that resolves adapter-validated item identities and exact claim resources, builds a minimal child environment, and starts via exec without claiming or shell evaluation. 3. Test hostile identifiers, disabled unresolved placeholders, option-like values, env isolation, and resource vectors; document trust boundary and run project gates before integration.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented strict launch config parsing, an exec-only handoff with exact resource/authority environment and default-deny ambient variables, and hostile-ID/option-like subprocess tests. Focused config/queue tests plus lint, format-check, full test, typecheck, and staged hooks passed; review and integration pending.

Review found an overbroad passEnv SESSION filter; corrected it to reserve only worker session identifiers and tested explicit AWS_SESSION_TOKEN. GitHub identity vector and exec-only child tests cover exact resources, hostile title/body and option-like IDs; newline IDs fail identity validation. Full lint/format-check/test/typecheck and staged hooks passed; source commits ddb7a1f and de591f0 merged to main at a34432f and 00de99f. D10 and plan section 12 remain consistent; no decision refinement required.

Post-completion review: no defects found in exec-only launch, argv substitution, allowlisted environment, passEnv, or unresolved-placeholder handling. No follow-up needed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added strictly validated queue launch configuration and an exec-only, minimal-environment handoff; verified identity vectors, hostile input and secret isolation with subprocess tests and all project gates. Reviewed and corrected passEnv session-token handling; merged into main at 00de99f.
<!-- SECTION:FINAL_SUMMARY:END -->
