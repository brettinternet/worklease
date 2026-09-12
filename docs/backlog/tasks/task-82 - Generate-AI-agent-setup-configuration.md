---
id: TASK-82
title: Generate AI-agent setup configuration
status: To Do
assignee: []
created_date: '2026-09-12 03:06'
labels: []
dependencies: []
references:
  - 'https://github.com/gastownhall/beads'
  - 'https://github.com/DmarshalTU/coord'
  - docs/mcp.md
priority: medium
type: enhancement
ordinal: 89000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Using Worklease from a coding agent currently requires manually assembling package installation, MCP configuration, agent instructions, authority location, and identity settings. A conservative setup generator should make the supported configuration discoverable while keeping every file change explicit and reviewable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The CLI can generate or print supported MCP and agent-instruction configuration for the documented coding-agent clients.
- [ ] #2 Generation defaults to previewing exact output and target paths; applying changes requires an explicit option.
- [ ] #3 Applying setup is idempotent, preserves unrelated configuration, refuses ambiguous or malformed existing configuration, and never writes credentials.
- [ ] #4 Generated configuration uses the installed Worklease command and version-matched instructions rather than embedding a stale lifecycle protocol.
- [ ] #5 Documentation covers preview, apply, manual installation, update, and removal workflows.
- [ ] #6 Tests cover fresh setup, repeated setup, existing unrelated configuration, malformed configuration, and safe removal of Worklease-owned entries.
<!-- AC:END -->
