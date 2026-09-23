---
id: TASK-131.1
title: Configure and execute launch actions safely
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
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
- [ ] #1 queue.yaml accepts `launch:` entries with a unique name, a non-empty argv, an optional cwd, and an optional passEnv list. Unknown placeholders and unknown keys are rejected with path-specific errors
- [ ] #2 Each placeholder is replaced within exactly one argv element with an adapter-validated identifier. The process starts with exec semantics and never through a shell
- [ ] #3 An action whose placeholders cannot be resolved for the item (for example `{checkout}` on an API-only source without an explicit cwd) is disabled with reason `unresolved-placeholder`
- [ ] #4 docs/queue.md warns that argv substitution prevents shell expansion but not target-program option parsing, and shows explicit value arguments or `--`. A test passes an identifier beginning with `-` and shows it is never parsed as an option by a sample launcher that uses `--`
- [ ] #5 The child environment contains only the allowlist, the passEnv variables, and the four WORKLEASE_* variables. A test with GH_TOKEN, GITHUB_TOKEN, and a canary secret in the parent environment proves none reach the child unless passEnv names them
- [ ] #6 WORKLEASE_PROFILE and WORKLEASE_QUEUE_RESOURCES carry the view's authority profile and the item's exact resources (matching the TASK-126.4 vectors)
- [ ] #7 Titles, bodies, and session IDs never appear in argv or the environment. A test with a hostile title and hostile IDs (containing spaces, quotes, `$()`, newlines, and leading `-`) shows no injection
- [ ] #8 docs/queue.md documents launch actions, the environment contract, and the trust model
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
