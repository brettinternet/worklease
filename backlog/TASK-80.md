---
id: TASK-80
title: Add optional agent mutation guard integrations
status: done
type: feature
priority: medium
blocked_by: [TASK-79]
rank: a9d31674c59d315ea
refs: ['https://github.com/ssheleg/agent-sync', 'https://github.com/dicklesworthstone/mcp_agent_mail']
created: 2026-09-12T03:06:00Z
updated: 2026-09-12T04:45:00Z
---

A verification primitive only helps when coding agents invoke it consistently. Optional, generated guard integrations should let supported agent harnesses block native edit or shell mutations when the current project has no valid Worklease claim, without making agent-specific hooks part of the lease authority or silently rewriting user configuration.

## Acceptance criteria

- [ ] The project provides an opt-in guard integration for at least one hook-capable coding-agent harness and a documented generic command-hook integration.
- [ ] A guarded mutation is allowed only when non-mutating verification succeeds for the configured project context; missing, expired, stale, and mismatched claims block with an actionable message.
- [ ] Installation or generation previews intended configuration before writing, does not overwrite unrelated user configuration, and supports clean removal of only Worklease-owned configuration.
- [ ] Guard documentation explains bypasses and race boundaries, including that unsupported tools and mutations occurring after verification are not fenced.
- [ ] Automated tests exercise allow, deny, installation, idempotency, preservation of unrelated configuration, and removal behavior.

## Notes

Closed 2026-09-12 as superseded before starting the unattended go-rewrite loop, so readiness-based selection cannot pick Python-era work. Acceptance criteria intentionally left unchecked: they were not delivered here. Delivery is owned by TASK-85.16.

## Summary

Superseded by TASK-85.16 in the Go rewrite (TASK-85). Not delivered in Python. Go Product Contract (docs/backlog/docs/go-rewrite/doc-2) section 13 (setup guard: preview by default, apply and remove, Claude Code PreToolUse hook, generic shell example) fixes the behavior.
