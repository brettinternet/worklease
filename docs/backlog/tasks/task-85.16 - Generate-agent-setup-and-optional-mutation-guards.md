---
id: TASK-85.16
title: Generate agent setup and optional mutation guards
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.14
  - TASK-85.15
references:
  - TASK-80
  - TASK-82
  - ../hum/internal/cli/mcp.go
parent_task_id: TASK-85
priority: medium
type: feature
ordinal: 108000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the Go-native MCP and verification workflow easy to install in coding-agent harnesses without silently rewriting user configuration. Combine the setup-generator intent from TASK-82 with opt-in pre-mutation guard integrations from TASK-80. Generated integrations remain cooperative helpers, not part of claim authority.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The CLI previews version-matched MCP and agent-instruction configuration for supported coding-agent clients and applies it only with an explicit option.
- [ ] #2 Apply and remove operations are idempotent, preserve unrelated configuration, reject ambiguous or malformed files, and write no credentials.
- [ ] #3 At least one hook-capable agent integration and one generic command-hook example use non-mutating claim verification to allow or block configured repository mutations.
- [ ] #4 Documentation clearly states bypass, time-of-check/time-of-use, unsupported-tool, direct-provider-write, and same-host boundaries.
- [ ] #5 Tests cover preview, fresh apply, repeat apply, malformed and unrelated configuration, precise removal, guard allow/deny cases, and version-matched generated content.
<!-- AC:END -->
