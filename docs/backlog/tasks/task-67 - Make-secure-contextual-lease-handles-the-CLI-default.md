---
id: TASK-67
title: Make secure contextual lease handles the CLI default
status: To Do
assignee: []
created_date: '2026-09-12 01:49'
updated_date: '2026-09-12 01:52'
labels: []
dependencies: []
priority: high
type: enhancement
ordinal: 71000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The safest Worklease lifecycle already uses a private lease file, but the simplest commands use token output and require callers to manually choose, retain, and repeat a path before they receive that safer behavior. This makes the recommended path more cumbersome than the less-safe path and encourages bearer tokens in shell plumbing.

Make a private, context-scoped handle under the existing Worklease state home the default for local CLI lifecycle commands. Worklease state already follows `--home`, `WORKLEASE_HOME`, `XDG_STATE_HOME/worklease`, and `~/.local/state/worklease`; mutable lease handles belong in state rather than `~/.config`, and must not be created in repositories. The implicit context should remain stable across subdirectories of one Git worktree while distinguishing linked worktrees, and should fall back predictably outside Git. Explicit paths and explicit stateless credential workflows remain available for concurrent leases, automation, and compatibility-sensitive callers.

The intended basic experience is `worklease acquire -r TASK-42`, followed from the same worktree by `worklease status`, `worklease heartbeat`, `worklease exec -- command`, and `worklease release -m complete`, without copying secrets or repeating a handle path. Explicit command input must always win over inference, and ambiguous mixtures must fail rather than silently selecting credentials.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A bare singleton or bundle acquire persists its handle beneath the resolved Worklease state home in a private mode-0700 handle directory and mode-0600 file, suppresses bearer-token output, and writes no lease state into the repository or caller directory.
- [ ] #2 Implicit handle selection uses a documented deterministic identity derived from the canonical Git worktree root, remains stable across its subdirectories and equivalent path spellings, distinguishes linked worktrees, and falls back to the canonical current directory outside Git.
- [ ] #3 The contextual handle supports the complete unambiguous singleton and bundle lifecycle without a repeated path: status, heartbeat, checkpoint, guarded execution, transfer, reconciliation, and release infer the stored resource set and mutation identity when no competing explicit input is supplied.
- [ ] #4 Credential precedence is consistent and documented: an explicit lease path wins; complete explicit resource/claim/revision/credential input preserves stateless operation; explicit resource input wins for read-only commands; and partial or conflicting explicit input fails actionably instead of being completed silently from the contextual handle.
- [ ] #5 `-L PATH` is accepted everywhere `--lease-file PATH` is accepted, with identical behavior, and explicit lease-file lifecycles remain backward compatible.
- [ ] #6 Acquire provides a clearly named long-form opt-out, `--no-lease-file`, that preserves the existing stateless success payload including its bearer token; it conflicts actionably with `-L` or `--lease-file`.
- [ ] #7 Attempting a second implicit acquire in the same context never silently overwrites or orphans an existing handle; the error identifies the active contextual handle without revealing secrets and explains how to release it or use `-L PATH` for concurrent leases.
- [ ] #8 Missing, stale, malformed, unsafe, wrong-kind, unwritable, and unusable contextual handles produce actionable diagnostics without exposing token material or silently falling back to a different credential mode; a successful release removes only the selected handle.
- [ ] #9 CLI help and examples, README lifecycle guidance, CLI reference, generated release documentation, agent-facing workflow guidance, and changelog explain the secure default, XDG state-home location, context rules, explicit-path override, concurrent-lease workflow, `--no-lease-file` compatibility path, and the breaking default change for existing scripts.
- [ ] #10 Automated tests cover singleton and bundle default lifecycles, Git root and subdirectory resolution, symlink-equivalent paths, linked-worktree isolation, non-Git scoping, home overrides, precedence and opt-out behavior, collision handling, permissions, token suppression, diagnostics, release cleanup, and compatibility with explicit lease files and complete explicit credentials.
<!-- AC:END -->
