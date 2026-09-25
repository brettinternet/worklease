---
id: TASK-144
title: Make `worklease queue init` a one-command setup
status: To Do
assignee: []
created_date: '2026-09-25 17:04'
updated_date: '2026-09-25 17:04'
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

Two parts of the TASK-137 design turned out wrong on review:

- `--apply` does not match how init commands behave. `server init`, `backlog init`, and `git init` write directly. In this CLI, preview-then-`--apply` is reserved for commands that edit third-party files (`setup mcp`, `setup guard`) or delete data (`gc`). `queue init` only creates or additively merges an owner-private file. Its one side effect, the initial identity confirmation, runs only when there is nothing to migrate. The risky decisions (`--portable-claims`, `--authority`) are already explicit flags, the same model `server init` uses with `--confirm-non-loopback`.
- A missing `me` principal needs a reliable default. `me` drives the `assigned: [me, nobody]` view filter and is the actor for Start work and "assign to me" (see queue_write_controller.go and queue_start.go). It cannot simply be optional: an empty `me` silently hides tasks assigned to the user as `assigned-elsewhere`. The OS login is the right fallback. In this repository `@$USER` gives `@brett`, which matches existing task assignees, while the GitHub login (`brettinternet`) does not.

Goal: in a typical Backlog.md or GitHub checkout, `worklease queue init` alone produces a working, confirmed configuration, and `worklease queue` is the only next step.

## Settled decisions (do not re-litigate)

- Remove `--apply` outright; do not keep a compatibility alias. The TASK-137 CHANGELOG entry is still under Unreleased, so nothing shipped depends on the flag. Add `--dry-run`, which does exactly what the current zero-flag preview does.
- The Backlog.md `me` resolution order: existing `me.backlog-md` entry, then `--me`, then a single-value `defaultAssignee`, then `@` + the OS login name (`os/user.Current().Username`, falling back to `$USER`). `me-required` remains only when none of these yields a valid `@name`.
- `me.backlog-md` is a list, so `--me` on a re-run adds the name to the list instead of reporting a conflict. It never removes or rewrites existing names. GitHub `me[host]` stays a single string that must match the authenticated `gh` account.
- The JSON envelope keeps its shape (`path`, `outcome`, `sourceId`, `facts`, `yaml`, `applied`, `identity`, `unmapped`, `checklist`, `nextCommands`). `applied` is false only under `--dry-run` or an unchanged outcome. TASK-143.5 (external sources in queue init) builds on this command and should use the new flags.

## Out of scope

External adapters (TASK-143.5), new adapters, changing which workflow statuses are mapped, and TUI onboarding.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Without `--dry-run`, `worklease queue init` writes or merges queue.yaml and runs the safe initial identity confirmation with the same conditions and code path as the current `--apply`. `--apply` is removed and reported as an unknown flag
- [ ] #2 `--dry-run` prints the detected facts, their origins, and the exact YAML, and creates no directory, file, lock, or identity record
- [ ] #3 When no `me.backlog-md` entry, `--me`, or single `defaultAssignee` exists, `me.backlog-md` defaults to `@` plus the OS login name. Its fact origin names the OS user. The run succeeds in a Backlog.md checkout whose `defaultAssignee` is empty
- [ ] #4 Re-running for an already configured Backlog.md source with a new `--me @name` adds that name to `me.backlog-md`, keeps existing names and comments, and reports outcome `merged`. An existing name reports `unchanged` and writes nothing. For GitHub, a `--me` different from the authenticated account is still an error
- [ ] #5 Generated and merged queue.yaml uses block-style YAML for `me`, `sources`, `views`, `workflow`, `claims`, and `filter`, with no flow-style collections introduced by init. Existing user formatting of untouched entries is preserved as far as yaml.v3 allows. The result still loads through `config.LoadQueue`
- [ ] #6 A successful write prints one concise summary: path, outcome, source ID and adapter, `me` value, mapped and unmapped workflow intents, and identity result, followed by the next command. The fact list and full YAML appear only with `--dry-run`. `--json` output is unchanged apart from the `applied` semantics
- [ ] #7 Suggested commands include only flags whose values differ from what init would detect or default to (for example, no `--checkout`, `--adapter`, or `--source-id` when detected). Values are shell-quoted only when necessary. When confirmation succeeds, the next command is `worklease queue`, or `worklease queue --view NAME` for a non-default view
- [ ] #8 Each fact origin names the input actually used (for example `--authority`, `default`, `backlog config get defaultAssignee`, `OS user`, or `git origin`), not a list of alternatives
- [ ] #9 When the `backlog` or `gh` executable is not on PATH, init reports that the named CLI is not installed or not on PATH, instead of a failure to read statuses or an authentication error, and writes nothing
- [ ] #10 Tests in internal/cli/queue_init_test.go cover: default write with no flags in a checkout with an empty `defaultAssignee`; `--dry-run` writes nothing; `--apply` rejected; `--me` appends on re-run; block-style output; the minimal next command; and missing `backlog`/`gh` binaries. Tests that only asserted the removed `--apply` split are updated, not duplicated
- [ ] #11 docs/queue.md, docs/cli-reference.md, the docs/cli-zero-flag-audit.md `queue init` row, command help examples, the CHANGELOG.md Unreleased entry, and D10 in docs/work-queue-tui-proposal.md describe direct writing with `--dry-run` preview and the `me` default. No document still mentions `queue init --apply`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 mise run lint, format-check, test, typecheck, and doc-test pass
- [ ] #2 New or changed tests pass `go test -race -count=3 -run TESTNAME ./internal/cli`
- [ ] #3 Only files changed for this task are staged; `mise run hooks` passes; committed with a concise message (no co-author trailer)
<!-- DOD:END -->
