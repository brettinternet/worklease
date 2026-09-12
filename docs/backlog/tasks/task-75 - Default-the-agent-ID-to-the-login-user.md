---
id: TASK-75
title: Default the agent ID to the login user
status: To Do
assignee: []
created_date: '2026-09-12 02:18'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - README.md
  - docs/cli-reference.md
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 82000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A bare `worklease acquire -r demo` fails with `missing-agent-id` unless `WORKLEASE_AGENT_ID` is exported or `-a` is passed. It is the last required flag on `acquire` besides the resource, and the claim model states that agent identity never grants adoption, so it is audit metadata rather than a security input. Requiring it makes the first command a human or agent types fail.

## Decision

Resolution order becomes `--agent-id`, then `WORKLEASE_AGENT_ID`, then the login user name from `getpass.getuser()`. When all three are unavailable the existing `missing-agent-id` error and hint remain. The same helper (`_default_agent_id` in `cli.py`) already serves `acquire`, `acquire-bundle`, and `transfer --successor-agent-id`, so all three inherit the default. The MCP server resolves its own identity from `WORKLEASE_AGENT_ID` or a startup argument and is unchanged.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease acquire -r RES` with no `-a` and no `WORKLEASE_AGENT_ID` succeeds and records `agentId` equal to the login user name; `WORKLEASE_AGENT_ID` when set takes precedence over the login name; `-a` wins over both.
- [ ] #2 The same precedence applies to `acquire-bundle` and to `transfer --successor-agent-id`.
- [ ] #3 When the login lookup fails and neither the flag nor the variable is set, the error remains `missing-agent-id` with the existing hint.
- [ ] #4 Help text for `--agent-id` and `--successor-agent-id` names the three-step default; the README lifecycle no longer exports `WORKLEASE_AGENT_ID` and mentions it as an optional override; `docs/cli-reference.md` and CHANGELOG `Unreleased` are updated; the MCP server identity resolution is untouched.
- [ ] #5 Tests cover flag, variable, and login-name precedence plus the lookup-failure path, and `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
