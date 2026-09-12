---
id: TASK-67
title: Make secure contextual lease handles the CLI default
status: To Do
assignee: []
created_date: '2026-09-12 01:49'
updated_date: '2026-09-12 02:08'
labels:
  - cli
  - ux
  - security
dependencies: []
references:
  - src/worklease/cli.py
  - src/worklease/lease_file.py
  - src/worklease/execution_context.py
  - src/worklease/sqlite.py
  - src/worklease/instructions.py
  - src/worklease/mcp_server.py
  - README.md
  - docs/cli-reference.md
  - skills/worklease-workflow/SKILL.md
  - CHANGELOG.md
priority: high
type: enhancement
ordinal: 71000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The safest Worklease lifecycle already uses a private lease handle, but the simplest commands emit a bearer token and require callers to mint, retain, and repeat a `--lease-file` path before they get the safer behavior. The recommended path is therefore more cumbersome than the less-safe path, which pushes tokens, claim IDs, and revisions into shell plumbing and agent transcripts.

Make a private, context-scoped handle under the Worklease state home the default for CLI lifecycle commands. Target experience, from any subdirectory of one Git worktree:

```sh
worklease acquire -r TASK-42
worklease status
worklease heartbeat
worklease exec -- command
worklease release -m complete
```

No path, token, claim ID, or revision is copied between commands, and no token appears in output.

## Decisions (settled here; subtasks implement, they do not re-open)

- **Location.** `<state home>/context-leases/<context-id>.lease`, where state home follows the existing `--home`, `WORKLEASE_HOME`, `XDG_STATE_HOME/worklease`, `~/.local/state/worklease` resolution in `lease_home()`. Directory mode 0700 through the existing `secure_directory`; file mode 0600 through the existing `lease_file` module. It is a sibling of the MCP server's `mcp-leases/`; the two never share handles. Nothing is ever written in the repository or caller directory.
- **Context identity.** The canonical root is `git rev-parse --show-toplevel` run from the caller directory with `GIT_*` routing variables stripped (as `execution_context._git_output` already does), then `Path.resolve(strict=True)`. When git is unavailable, the directory is not inside a work tree (bare repository, inside `.git`), or the probe fails, the root is the resolved caller directory. The context ID is the lowercase SHA-256 hex digest of the root path encoded as UTF-8. Linked worktrees have distinct roots and therefore distinct handles; a submodule checkout is its own context. The lease-file schema stays at version 1; diagnostics recompute the root instead of storing it.
- **Precedence for mutations** (`heartbeat`, `checkpoint`, `exec`, `release`, `transfer`, `reconcile-operation`, and bundle variants). `-L`/`--lease-file PATH` wins and keeps the current per-field override semantics. Otherwise, if none of resource(s), claim ID, revision, token, token-file, or token-fd is given, the contextual handle is required. If all of resource(s), claim ID, revision, and exactly one credential are given, the command runs stateless and the contextual handle is neither read nor written. Any other partial mixture fails `lease-context-conflict` (exit 64). A required handle that does not exist fails `lease-context-missing` (exit 64). Non-identity flags (`--ttl`, `--operation-id`, `--checkpoint`, `--reason`, `--max-duration`, execution directory flags, successor flags) never trigger the conflict.
- **Read-only commands** (`status`, `status-bundle`). Explicit `--resource` wins; otherwise resource(s) come from `-L PATH` or the contextual handle. A singleton command reading a bundle handle, or the reverse, fails `lease-file-kind-mismatch` with a hint naming the matching command. `history` is deliberately excluded and keeps requiring `--resource`; cross-resource inspection is the new `events` command in TASK-69.
- **Acquire.** The default writes the contextual handle, suppresses the token, and reports the handle path as `leaseFile` in the JSON payload (`LEASE_FILE` in text). `-L PATH` writes an explicit handle instead. `--no-lease-file` restores the current stateless token-bearing payload for automation. `-L` combined with `--no-lease-file` is an error.
- **Collision.** Reuse the existing `lease-file-in-use` protection for the contextual destination. A handle naming a live claim is never overwritten; the error names the handle path and context root and points to `release` or `-L PATH` for a concurrent lease. A handle whose claim is no longer active is replaced.
- **Compatibility.** `--lease-file`, `--successor-lease-file`, and complete stateless credential workflows keep working unchanged. The default change is breaking for scripts that parse `claim.token` from a bare `acquire`; the next release is 0.10.0 and the changelog says so.

## Non-goals

MCP server behavior, the lease-file schema, cross-home handle discovery, adopting handles by agent identity, `history`, `list`, and `gc` are unchanged.

## Sequence

Subtasks are ordered by dependency: TASK-67.1 (context resolution module, no CLI change) then TASK-67.2 (CLI default, precedence, diagnostics, tests, changelog) then TASK-67.3 (README, CLI reference, agent instructions, skill and Backlog guide, MCP note). Each subtask carries its own acceptance criteria; the parent criteria are the integrated outcome.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 From a subdirectory of a Git worktree, `worklease acquire -r RES` followed by `status`, `heartbeat`, `checkpoint -k {}`, `exec -- true`, and `release -m done` all succeed with no lease path, token, claim ID, or revision on the command line, and no token appears in any output.
- [ ] #2 The same path-free lifecycle works for bundles through `acquire-bundle`, `status-bundle`, `heartbeat-bundle`, `exec-bundle`, and `release-bundle`.
- [ ] #3 The handle lives under `<state home>/context-leases/` with directory mode 0700 and file mode 0600, follows `--home` and `WORKLEASE_HOME`, and the repository and caller directory receive no new files.
- [ ] #4 Existing `--lease-file` scripts and complete stateless credential scripts run unchanged, and `acquire --no-lease-file` returns the token-bearing stateless payload.
- [ ] #5 README, CLI reference, `worklease instructions`, the workflow skill and Backlog guide, command help, generated man page source, and CHANGELOG describe the default, its location and context rules, `-L PATH`, `--no-lease-file`, and the breaking change.
- [ ] #6 TASK-67.1, TASK-67.2, and TASK-67.3 are Done and `mise run lint`, `mise run format-check`, `mise run test`, and `mise run typecheck` pass.
<!-- AC:END -->
