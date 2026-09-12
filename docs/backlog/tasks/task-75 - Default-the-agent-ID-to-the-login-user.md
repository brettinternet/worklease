---
id: TASK-75
title: Default the agent ID to the login user
status: Done
assignee: []
created_date: '2026-09-12 02:18'
updated_date: '2026-09-12 04:44'
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
  - scripts/release_docs.py
priority: medium
type: enhancement
ordinal: 82000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A bare `worklease acquire -r demo` fails with `missing-agent-id` unless `WORKLEASE_AGENT_ID` is exported or `-a` is passed. It is the last required flag on `acquire` besides the resource, and the claim model states that agent identity never grants adoption, so it is audit metadata rather than a security input. Requiring it makes the first command a human or agent types fail.

## Decision

For an omitted agent option, resolution becomes a nonblank `WORKLEASE_AGENT_ID`, then the login name from `getpass.getuser()`. An explicit nonblank `--agent-id` or `--successor-agent-id` continues to win because defaults are applied only when the parsed option is `None`; an explicitly blank option is not replaced and continues to fail model validation. `_default_agent_id` catches login lookup failure and rejects a blank lookup result. If neither source yields a value, the existing `missing-agent-id` error and option-specific hint remain. The same helper already serves `acquire`, `acquire-bundle`, and `transfer --successor-agent-id`, so all three inherit the fallback. The MCP server resolves its own identity from `WORKLEASE_AGENT_ID` or a startup argument and is unchanged.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease acquire -r RES` with no `-a` and no nonblank `WORKLEASE_AGENT_ID` succeeds and records `agentId` equal to `getpass.getuser()`; a nonblank `WORKLEASE_AGENT_ID` takes precedence over the login name, and an explicit nonblank `-a` wins over both.
- [ ] #2 The same omitted-option fallback and precedence apply to `acquire-bundle` and `transfer --successor-agent-id`.
- [ ] #3 When `getpass.getuser()` raises `OSError` or returns blank and no nonblank environment override exists, the command exits 64 with `missing-agent-id` and the existing option-specific hint; an explicitly blank agent option is not defaulted and continues to fail with `invalid-agent-id` or `invalid-successor-agent-id`.
- [ ] #4 Help for `--agent-id` and `--successor-agent-id` names the environment and login-name fallback; the README lifecycle and generated manual examples no longer require `WORKLEASE_AGENT_ID` and mention it as an optional override; `docs/cli-reference.md` and CHANGELOG `Unreleased` are updated; generated release documentation renders successfully; the MCP identity contract is unchanged.
- [ ] #5 Tests mock the environment and login lookup to cover flag, variable, and login-name precedence, blank and raising lookup failures, explicit blank options, all three affected commands, and option-specific hints; `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Closed 2026-09-12 as superseded before starting the unattended go-rewrite loop, so readiness-based selection cannot pick Python-era work. Acceptance criteria intentionally left unchecked: they were not delivered here. Delivery is owned by TASK-85.14.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Superseded by TASK-85.14 in the Go rewrite (TASK-85). Not delivered in Python. Go Product Contract (docs/backlog/docs/go-rewrite/doc-2) section 5 (agent ID resolves from --agent, WORKLEASE_AGENT_ID, YAML agent_id, then the OS user) fixes the behavior.
<!-- SECTION:FINAL_SUMMARY:END -->
