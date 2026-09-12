---
id: TASK-85.16
title: Generate agent setup and optional mutation guards
status: To Do
assignee: []
created_date: '2026-09-12 03:24'
updated_date: '2026-09-12 04:47'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.14
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
Installing Worklease in a coding-agent harness means editing client configuration by hand and remembering the verify step. Contract section 13 specifies conservative generators: preview by default, explicit `--apply`, semantic preservation of unrelated configuration, precise `--remove`, and an opt-in Claude Code PreToolUse guard that calls `worklease verify --hook claude-code`. Generated files are cooperative helpers, never part of claim authority. This task absorbs TASK-82 and TASK-80, both closed as superseded.

Read first: contract sections 4 (setup rows), 10.3 (hook input parsing, Bash allowlist, exit semantics), 13. Evidence: the TASK-80 and TASK-82 acceptance criteria; `docs/mcp.md` (the current Claude Code snippet). Patterns: `../hum/internal/cli/init.go` and `init_test.go` (file generation with preview), `../hum/internal/cli/manifest.go` (JSON handling). Before implementing, confirm the Claude Code hook stdin shape (`cwd`, `tool_name`, `tool_input`) and exit-code semantics against the current Claude Code hooks documentation and record the source URL in the task notes.

Deliver in `internal/setup`: client targets (claude-code project and user, cursor project and user, generic) with target path resolution; `Plan(target, action)` producing the intended JSON document and a unified diff; `Apply` writing atomically with two-space indentation while preserving all unrelated keys; `Remove` deleting only worklease-owned entries; the Claude Code guard hook entry with the default matcher `Edit|Write|MultiEdit|NotebookEdit`, the `--include-bash` option that extends it to `Edit|Write|MultiEdit|NotebookEdit|Bash`, and the exact command string `worklease verify --hook claude-code`; the instructions block generator with version markers; a generic POSIX shell guard example. Deliver in `internal/cli`: `setup mcp`, `setup guard`, `setup instructions`. Help text must state the bypass, time-of-check versus time-of-use, unsupported-tool, provider-write, and same-host boundaries, and explain why Bash is opt-in (TASK-85.17 copies these into the docs).

Owned paths: `internal/setup`, `internal/cli/setup.go` and tests. Out of scope: Codex or other TOML-configured clients (documented as unsupported), installing the binary itself, the verify hook logic itself (TASK-85.12).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Preview tests prove `setup mcp --client claude-code` and `--client cursor` print the target path and a unified diff without writing, `--client generic` prints the JSON snippet, the generated command is the absolute path of the running binary with args ["mcp"], and WORKLEASE_AGENT_ID appears only when --agent is given.
- [ ] #2 Apply tests prove a fresh apply creates the file with the worklease entry, a second apply is a no-op producing a byte-identical file, applying to a file with unrelated mcpServers entries and top-level keys preserves them semantically, a non-object root, invalid JSON, or an existing mcpServers.worklease of a different type fails setup-config-malformed without writing, and writes are atomic through a temp file plus rename.
- [ ] #3 Remove tests prove --remove deletes only mcpServers.worklease (and only the worklease hook entry for guard), leaves other entries intact, and is idempotent when the entry is absent.
- [ ] #4 Guard tests prove the generated Claude Code hook entry matches contract 13 exactly (matcher Edit|Write|MultiEdit|NotebookEdit by default and Edit|Write|MultiEdit|NotebookEdit|Bash with --include-bash), that running the generated hook command with a valid contextual handle exits 0, with a missing or expired handle exits 2 with a one-line stderr message, and with a Bash event whose command starts with worklease or backlog exits 0 even without a claim (tests execute the real `worklease verify --hook claude-code` with sample hook JSON on stdin); the generic shell example is executable and blocks when verify fails.
- [ ] #5 `setup instructions` output includes version markers matching `worklease version` and the instructions text, help text for all setup commands states the boundary caveats and the Bash opt-in rationale, and `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
