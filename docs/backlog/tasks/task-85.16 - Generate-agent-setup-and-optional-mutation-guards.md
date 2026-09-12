---
id: TASK-85.16
title: Generate agent setup and optional mutation guards
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 19:59'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.15
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - docs/mcp.md
  - ../hum/internal/cli/init.go
  - ../hum/internal/cli/manifest.go
  - TASK-80
  - TASK-82
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 108000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Generate agent setup and optional native edit guards from contract section 13. Own internal/setup and setup CLI. Validate current client documentation and record URLs/fixtures for each supported project/user configuration shape; do not assume a user's global configuration is the project's JSON schema.

Preview by default, apply/remove explicitly, preserve unrelated keys and reject unsafe paths/concurrent file changes. Propagate explicit authority home/config and session/handle selection, with safely quoted absolute binary paths. Native guards match Edit/Write/MultiEdit/NotebookEdit only; by default the generated hook verifies a current valid claim for the selected context or session (`--coverage claim`), and `setup guard --coverage path` generates the stricter per-file path-coverage mode. No include-bash flag, shell-string parsing or command-name bypass. Document explicit MCP lease-to-hook binding and check-to-edit limits.

Evidence and patterns (the amended contract is normative): TASK-80 and TASK-82 (closed as superseded) record the guard and setup intent; `docs/mcp.md` holds the current Claude Code snippet. hum patterns: `internal/cli/init.go` and `init_test.go` (file generation with preview), `internal/cli/manifest.go` (JSON handling). Before implementing, confirm the Claude Code PreToolUse stdin fields (`cwd`, `tool_name`, `tool_input`) and exit-code semantics, and the Claude Code and Cursor user-scope configuration shapes, against the current client documentation and record the URLs in the task notes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Preview outputs the correct documented target and diff/snippet without writes for each supported client/scope, including fixtures for user-level configuration.
- [ ] #2 Apply/remove are atomic and idempotent, preserve unrelated content semantically, reject malformed types/symlinked or unsafe files and detect a concurrent edit before replacement.
- [ ] #3 Generated commands use the safely quoted absolute running binary and preserve explicit home/config/session selection; CLI/MCP/hooks resolve the same authority in integration tests.
- [ ] #4 Native guard integration executes the real verify hook in both modes: the default claim-coverage entry allows edits under a valid claim and blocks missing/expired/pending claims, the --coverage path entry additionally blocks unrelated paths, both support explicit MCP reference binding, removal recognizes either generated command form, and Bash is not registered.
- [ ] #5 Setup instructions/version markers and a generic wrapper match the contract; final surface tests now include all commands, documentation states supported boundaries, and mise run ci-go passes.
- [ ] #6 A new-user setup journey previews then explicitly applies MCP configuration and completes a minimal lease lifecycle without installing native guards or manually editing credentials; generic output explains the required client action and all setup help presents guards as optional.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add internal/setup JSON generators and atomic preview/apply/remove operations for Claude Code, Cursor, and generic output.
2. Add setup CLI commands and real Claude Code verify-hook parsing with claim/path coverage and explicit authority selection.
3. Add fixtures and integration/surface tests for client scopes, mutation safety, authority consistency, hook behavior, and onboarding.
4. Update MCP/setup documentation with verified client URLs and safety boundaries; run focused tests and mise run ci-go.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Current client documentation verified before implementation:
- Claude Code hooks/input/exit semantics and hook locations: https://docs.anthropic.com/en/docs/claude-code/hooks (PreToolUse command hooks receive JSON stdin including cwd, tool_name, and tool_input; exit 2 blocks, exit 0 allows).
- Claude Code user settings: https://docs.anthropic.com/en/docs/claude-code/settings (~/.claude/settings.json); user MCP state uses ~/.claude.json as documented there.
- Cursor MCP installation: https://cursor.com/docs/context/mcp/install-links (project .cursor/mcp.json and global ~/.cursor/mcp.json).
- Cursor hooks/configuration: https://cursor.com/docs/agent/hooks (~/.cursor/hooks.json is the user hook shape; native guard generation is not claimed for Cursor).
<!-- SECTION:NOTES:END -->
