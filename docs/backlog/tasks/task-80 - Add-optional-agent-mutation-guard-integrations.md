---
id: TASK-80
title: Add optional agent mutation guard integrations
status: To Do
assignee: []
created_date: '2026-09-12 03:06'
labels: []
dependencies:
  - TASK-79
references:
  - 'https://github.com/ssheleg/agent-sync'
  - 'https://github.com/dicklesworthstone/mcp_agent_mail'
priority: medium
type: feature
ordinal: 87000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A verification primitive only helps when coding agents invoke it consistently. Optional, generated guard integrations should let supported agent harnesses block native edit or shell mutations when the current project has no valid Worklease claim, without making agent-specific hooks part of the lease authority or silently rewriting user configuration.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The project provides an opt-in guard integration for at least one hook-capable coding-agent harness and a documented generic command-hook integration.
- [ ] #2 A guarded mutation is allowed only when non-mutating verification succeeds for the configured project context; missing, expired, stale, and mismatched claims block with an actionable message.
- [ ] #3 Installation or generation previews intended configuration before writing, does not overwrite unrelated user configuration, and supports clean removal of only Worklease-owned configuration.
- [ ] #4 Guard documentation explains bypasses and race boundaries, including that unsupported tools and mutations occurring after verification are not fenced.
- [ ] #5 Automated tests exercise allow, deny, installation, idempotency, preservation of unrelated configuration, and removal behavior.
<!-- AC:END -->
