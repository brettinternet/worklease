---
id: TASK-144
title: Make `worklease queue init` a one-command setup
status: Done
assignee: []
created_date: '2026-09-25 17:04'
updated_date: '2026-09-25 18:10'
labels:
  - work-queue
milestone: m-1
dependencies: []
references:
  - internal/cli/queue_init.go
  - internal/cli/queue_init_test.go
  - internal/cli/queue_write_controller.go
  - internal/cli/queue_start.go
  - internal/cli/hosted_commands.go
  - internal/queue/backlog.go
  - internal/queue/github.go
documentation:
  - docs/queue.md
  - docs/cli-zero-flag-audit.md
  - docs/cli-reference.md
  - docs/work-queue-tui-proposal.md
priority: medium
type: enhancement
ordinal: 78000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Why

TASK-137 shipped `worklease queue init`, but the default path does not work in this repository. Running it here stops with `me-required`, because Backlog.md `defaultAssignee` is empty, which is the common case. The suggested next command is `worklease queue init --checkout /abs/path --adapter backlog-md --source-id worklease --me @name --apply`, which echoes every detected value back with a placeholder. The generated YAML is also flow style (`sources: [{id: ..., ...}]`) in a file people hand-edit.

Three parts of the TASK-137 design turned out wrong on review:

- `--apply` does not match how init commands behave. `server init`, `backlog init`, and `git init` write directly. In this CLI, preview-then-`--apply` is reserved for commands that edit third-party files (`setup mcp`, `setup guard`) or delete data (`gc`). `queue init` only creates or additively merges an owner-private file. Its one side effect, the initial identity confirmation, runs only when there is nothing to migrate. The risky decisions (`--portable-claims`, `--authority`) are already explicit flags, the same model `server init` uses with `--confirm-non-loopback`.
- A missing `me` principal needs a reliable default. `me` drives the `assigned: [me, nobody]` view filter and is the actor for Start work and "assign to me" (see queue_write_controller.go and queue_start.go). It cannot simply be optional: an empty `me` silently hides tasks assigned to the user as `assigned-elsewhere`. The OS login is the right fallback. In this repository `@$USER` gives `@brett`, which matches existing task assignees, while the GitHub login (`brettinternet`) does not.
- Init does not check the prerequisites the adapter enforces. `BacklogAdapter.Resolve` (internal/queue/backlog.go) fails for a missing `backlog` CLI (`cli-missing`), a version other than 1.52.x (`unsupported-version`), missing Git, or a project that enables `remoteOperations`/`checkActiveBranches` without `allowGitNetwork: true` (`git-network-consent`). Init always writes `allowGitNetwork: false` and runs none of these checks, so it can write a config that fails on the first queue read. Init also detects Backlog.md through `queue.BacklogDirectory`, which accepts a bare `backlog/` or `.backlog/` folder that `Resolve` rejects. Removing the `--apply` preview step makes this gap more costly, so it is fixed here.

Goal: in a typical Backlog.md or GitHub checkout, `worklease queue init` alone produces a working, confirmed configuration, and `worklease queue` is the only next step. When it cannot, it writes nothing and names the exact fix.

## Settled decisions (do not re-litigate)

- Remove `--apply` outright; do not keep a compatibility alias. The TASK-137 CHANGELOG entry is still under Unreleased, so nothing shipped depends on the flag. Add `--dry-run`, which does exactly what the current zero-flag preview does.
- The Backlog.md `me` resolution order: existing `me.backlog-md` entry, then `--me`, then a single-value `defaultAssignee`, then `@` + the OS login name (`os/user.Current().Username`, falling back to `$USER`). `me-required` remains only when none of these yields a valid `@name`.
- `me.backlog-md` is a list, so `--me` on a re-run adds the name to the list instead of reporting a conflict. It never removes or rewrites existing names. GitHub `me[host]` stays a single string that must match the authenticated `gh` account.
- Choose the adapter from repository evidence, never from installed tools. Falling back to GitHub when `backlog` is missing would silently change both the work source and the claim domain; undoing it later means a second source and an identity migration.
- Preflight through the real adapter `Resolve` (it has no side effects) instead of re-implementing its checks, so init and the queue cannot disagree.
- Remote Git access for a Backlog.md project is an explicit decision: add `--allow-git-network`; never enable it from detection alone.
- When both Backlog.md and GitHub are detected, Backlog.md wins and the output points at the other; never add both automatically, because GitHub brings network reads and its own identity.
- The JSON envelope keeps its shape (`path`, `outcome`, `sourceId`, `facts`, `yaml`, `applied`, `identity`, `unmapped`, `checklist`, `nextCommands`). `applied` is false only under `--dry-run` or an unchanged outcome. TASK-143.5 (external sources in queue init) builds on this command and should use the new flags.

## Out of scope

External adapters (TASK-143.5), new adapters, changing which workflow statuses are mapped, TUI onboarding, and widening the adapter Backlog.md version requirement.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Without `--dry-run`, `worklease queue init` writes or merges queue.yaml and runs the safe initial identity confirmation with the same conditions and code path as the current `--apply`. `--apply` is removed and reported as an unknown flag
- [x] #2 `--dry-run` prints the detected facts, their origins, and the exact YAML, and creates no directory, file, lock, or identity record
- [x] #3 When no `me.backlog-md` entry, `--me`, or single `defaultAssignee` exists, `me.backlog-md` defaults to `@` plus the OS login name. Its fact origin names the OS user. The run succeeds in a Backlog.md checkout whose `defaultAssignee` is empty
- [x] #4 Re-running for an already configured Backlog.md source with a new `--me @name` adds that name to `me.backlog-md`, keeps existing names and comments, and reports outcome `merged`. An existing name reports `unchanged` and writes nothing. For GitHub, a `--me` different from the authenticated account is still an error
- [x] #5 Generated and merged queue.yaml uses block-style YAML for `me`, `sources`, `views`, `workflow`, `claims`, and `filter`, with no flow-style collections introduced by init. Existing user formatting of untouched entries is preserved as far as yaml.v3 allows. The result still loads through `config.LoadQueue`
- [x] #6 A successful write prints one concise summary: path, outcome, source ID and adapter, `me` value, mapped and unmapped workflow intents, and identity result, followed by the next command. The fact list and full YAML appear only with `--dry-run`. `--json` output is unchanged apart from the `applied` semantics
- [x] #7 Suggested commands include only flags whose values differ from what init would detect or default to (for example, no `--checkout`, `--adapter`, or `--source-id` when detected). Values are shell-quoted only when necessary. When confirmation succeeds, the next command is `worklease queue`, or `worklease queue --view NAME` for a non-default view
- [x] #8 Each fact origin names the input actually used (for example `--authority`, `default`, `backlog config get defaultAssignee`, `OS user`, or `git origin`), not a list of alternatives
- [x] #9 Backlog.md is detected only when the checkout has `backlog.config.yml` or `backlog/config.yml`, the same evidence `BacklogAdapter.Resolve` requires. A bare `backlog/` or `.backlog/` folder is not a Backlog.md project
- [x] #10 The adapter is chosen from repository evidence only, never from which CLIs are installed. A detected Backlog.md project whose `backlog` CLI is missing is an error, not a fallback to GitHub
- [x] #11 Before writing, init resolves the proposed source through the adapter `Resolve` used by the queue and writes nothing when it fails. Output names the specific fix: `cli-missing` names the missing `backlog` or `gh` CLI and that 1.52.x is required; `unsupported-version` shows found and required versions; `git-missing` names Git; `git-network-consent` names `--allow-git-network`; GitHub authentication names `gh auth login --hostname HOST`. `--dry-run` runs the same check and reports the same result
- [x] #12 `--allow-git-network` writes `allowGitNetwork: true` and is recorded as a fact with origin `--allow-git-network`. Without it `allowGitNetwork` is written `false`, and a project with `remoteOperations` or `checkActiveBranches` enabled fails the preflight with `git-network-consent`
- [x] #13 When both a Backlog.md project and a GitHub origin are detected, init configures Backlog.md and the output names the unchosen source with the `--adapter github` command that adds it. It never adds both automatically
- [x] #14 Tests in internal/cli/queue_init_test.go cover: default write with no flags in a checkout with an empty `defaultAssignee`; `--dry-run` writes nothing; `--apply` rejected; `--me` appends on re-run; block-style output; the minimal next command; bare `backlog/` folder not detected; each preflight failure (missing `backlog`, unsupported version, network consent with and without `--allow-git-network`, missing `gh` or unauthenticated) writes nothing; and the both-detected hint. Tests that only asserted the removed `--apply` split are updated, not duplicated
- [x] #15 docs/queue.md, docs/cli-reference.md, the docs/cli-zero-flag-audit.md `queue init` row, command help examples, the CHANGELOG.md Unreleased entry, and D10 in docs/work-queue-tui-proposal.md describe direct writing with `--dry-run` preview, the `me` default, the preflight, and `--allow-git-network`. No document still mentions `queue init --apply`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 mise run lint, format-check, test, typecheck, and doc-test pass
- [x] #2 New or changed tests pass `go test -race -count=3 -run TESTNAME ./internal/cli`
- [x] #3 Only files changed for this task are staged; `mise run hooks` passes; committed with a concise message (no co-author trailer)
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Trace init and adapter contracts, then implement direct write, defaults, preflight and block YAML in isolated worktree. 2. Update focused tests and documentation. 3. Run race and repository gates; review, commit, merge to main, record evidence and finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented direct queue init write and dry-run, OS login fallback, append-only Backlog principals, adapter Resolve preflight, explicit network consent, block YAML, concise output and docs in task-144-queue-init worktree. Focused TestQueueInit race count=3 and lint/format-check/test/typecheck/doc-test passed; review underway.

Review: one proportional source review found concrete next-command defects (confirmation recovery view, both-detected hint, redundant flags, wrong view); fixed and reran focused checks. Verified lint, format-check, test, typecheck, doc-test; go test -race -count=3 -run ^TestQueueInit ./internal/cli; staged-only mise run hooks and commit hook. Implementation f9eb303 rebased as 9f99fbb and merged to main. No blocker; next step: use worklease queue.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Queue init writes directly after adapter preflight, defaults Backlog.md identity to OS login, emits block YAML and concise next steps; --dry-run previews safely. Verified focused race tests, full repository gates, doc-test and hooks; merged as 9f99fbb.
<!-- SECTION:FINAL_SUMMARY:END -->
