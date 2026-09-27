---
id: TASK-151
title: Let a repository propose queue sources for the user to adopt
status: Done
assignee: []
created_date: '2026-09-27 05:56'
updated_date: '2026-09-27 16:40'
labels:
  - work-queue
  - reviewed
dependencies: []
references:
  - internal/cli/queue_init.go
priority: low
type: feature
ordinal: 91000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Why

Queue configuration lives only in the owner-private `$XDG_CONFIG_HOME/worklease/queue.yaml` (D10). `config.QueuePath` deliberately ignores the checkout, the working directory, and Worklease environment variables, because that file controls launch argv, external adapter executables, credential helpers, checkout paths, and Git network consent. A repository that could supply any of those would get code execution just by being opened.

That leaves no way for a project to share its work sources with collaborators or agents. Each person must rediscover them, and in particular must independently choose the same portable claim domain, or their claims silently stop contending. The proposal already defers a narrow alternative (docs/work-queue-tui-proposal.md, "Project-suggested sources are deferred"): a repository file may only propose sources, which the user reviews and copies into their own queue.yaml.

## Decisions

**File.** `.config/worklease/queue-sources.yaml` at the Git worktree root of the checkout, committed with the project. It follows the `.config/<tool>/` project convention (as `.config/wt.toml` does here) and is deliberately not named `queue.yaml`, so it cannot be mistaken for loaded configuration. Shape:

```yaml
version: 1
sources:
  - id: worklease            # suggested; init de-duplicates as it does today
    adapter: backlog-md      # backlog-md or github only
    workflow: {start: In Progress, complete: Done, reopen: To Do}
    claims: {policy: generic, source: worklease}   # optional portable domain
```

Decoding is an allowlist: `version`, and per source `id`, `adapter`, `workflow`, and `claims`. Everything else is rejected by name, including `checkout`, `me`, `views`, `authority`, `launch`, `executable`, adapter config, `account`, credential helpers, and `allowGitNetwork`. A `github` source carries no host or repository; init derives both from the checkout's `origin`, exactly as detection does, so a proposal cannot point the user's `gh` credentials at another host or repository. At most 8 sources.

**Surface.** Only `worklease queue init` reads the file, and only for the checkout it is resolving (`--checkout PATH` or the current directory). When present, the proposal replaces provider detection as the source of facts, and each fact's origin names the file. Every existing init behavior still applies: preview and `--dry-run`, source ID de-duplication, `me` resolution, the default views or `--view`, `--authority`, `allowGitNetwork: false` unless the user passes `--allow-git-network`, identity confirmation, and portable claims skipping automatic confirmation. `--ignore-proposal` falls back to detection. `worklease queue`, the TUI, MCP, and `queue next` never read it; there is no passive notice.

**Changes after adoption.** Adopted sources are ordinary user configuration with no link back to the file. Re-running init on the checkout matches proposed sources to configured ones by checkout path and adapter: new proposed sources are added through the normal preview and write; a differing `workflow` or `claims` on an already-configured source is reported with the exact YAML difference and never applied; a changed `claims` also points to the identity migration checklist; sources dropped from the proposal are reported, not removed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `worklease queue init` in a checkout with `.config/worklease/queue-sources.yaml` previews every proposed source with that file as each fact's origin, and writes owner-private queue.yaml only when not `--dry-run`; `--ignore-proposal` uses provider detection instead.
- [x] #2 No command other than `queue init` reads the file: opening the queue or TUI, `queue query`, `queue next`, and MCP tools behave identically with and without it.
- [x] #3 The proposal decoder accepts only `version` and per-source `id`, `adapter` (`backlog-md` or `github`), `workflow`, and `claims`, with at most 8 sources. Any other key, including checkout, me, views, authority, launch, executable, adapter config, account, credential helpers, and allowGitNetwork, fails with an error naming the key, and queue.yaml is left unchanged.
- [x] #4 A proposed `github` source takes its host and repository only from the checkout's `origin`; init refuses the source when there is no GitHub origin.
- [x] #5 Adopted sources pass through the existing init rules unchanged: ID de-duplication, `me` resolution, default views or `--view`, `--authority`, `allowGitNetwork: false` without `--allow-git-network`, automatic identity confirmation only for new host-local local-authority sources, and the printed confirm command otherwise.
- [x] #6 Re-running init after adoption adds newly proposed sources, reports a differing workflow or claims on an existing source (and the migration checklist for claims) without applying it, reports sources removed from the proposal without removing them, and exits 0 with nothing written when nothing is new.
- [x] #7 docs/queue.md documents the file, its allowlist and trust boundary, and the adoption and re-run behavior; docs/work-queue-tui-proposal.md replaces the deferred note with the shipped design; CHANGELOG.md Unreleased has an Added entry.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add strict, bounded proposal parsing at checkout root; reject privileged or unknown fields before writes.
2. Extend queue init to prepare all proposed sources against an in-memory YAML document, preserving existing init preflight, identity, and views; report drift/removals without applying.
3. Test trust boundary, multi-source adoption, repeat/drift, detection bypass, and GitHub origin; update docs and changelog.
4. Run project gates, review once, commit in worktree, merge main, push, and verify CI.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented strict proposal parsing and multi-source init preparation, plus focused adoption/rerun/trust-boundary tests and documentation; focused queue init tests pass. Running full gates and one risk-focused review.

Full lint, format-check, typecheck, test, race, and doc-test passed; focused proposal tests passed with -race -count=3. One risk-focused review found three item-scoped defects in preview commands and mixed identity reporting; fixed all three and added focused regression checks. No second general review.

Evidence: TestQueueInitProposal* (focused -race -count=3), existing TestQueueInit* and full package suite exercise preview/adoption, detection bypass, strict key rejection, GitHub origin, init safety/identity, additions, drift, and removals; malformed proposal leaves queue query working; full CI run 36300756584 on merged main 4e5a073 succeeded across linux-x64/linux-arm64/macos-x64/macos-arm64 including queue/TUI/MCP suites, quality/e2e; mise run doc-test passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented explicit, bounded checkout source proposals for queue init with owner-private adoption, non-mutating drift/removal reports, strict trust boundary, documentation, and regression tests. Merged d179d77 into main as 4e5a073; focused race, full local gates and CI run 36300756584 passed.
<!-- SECTION:FINAL_SUMMARY:END -->
