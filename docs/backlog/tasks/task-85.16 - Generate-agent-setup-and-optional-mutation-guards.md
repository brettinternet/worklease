---
id: TASK-85.16
title: Generate agent setup and optional mutation guards
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 06:07'
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

Preview by default, apply/remove explicitly, preserve unrelated keys and reject unsafe paths/concurrent file changes. Propagate explicit authority home/config and session/handle selection, with safely quoted absolute binary paths. Native guards match Edit/Write/MultiEdit/NotebookEdit only and verify all target path resources. No include-bash flag, shell-string parsing or command-name bypass. Document explicit MCP lease-to-hook binding and check-to-edit limits.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Preview outputs the correct documented target and diff/snippet without writes for each supported client/scope, including fixtures for user-level configuration.
- [ ] #2 Apply/remove are atomic and idempotent, preserve unrelated content semantically, reject malformed types/symlinked or unsafe files and detect a concurrent edit before replacement.
- [ ] #3 Generated commands use the safely quoted absolute running binary and preserve explicit home/config/session selection; CLI/MCP/hooks resolve the same authority in integration tests.
- [ ] #4 Native guard integration executes real verify with valid claims and path coverage, blocks unrelated/missing/expired/pending claims, and supports explicit MCP reference binding; Bash is not registered.
- [ ] #5 Setup instructions/version markers and a generic wrapper match the contract; final surface tests now include all commands, documentation states supported boundaries, and mise run ci-go passes.
- [ ] #6 A new-user setup journey previews then explicitly applies MCP configuration and completes a minimal lease lifecycle without installing native guards or manually editing credentials; generic output explains the required client action and all setup help presents guards as optional.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
