---
id: TASK-102
title: Finish Go CLI help and command discovery ergonomics
status: Done
assignee:
  - '@brett'
created_date: '2026-09-13 04:08'
updated_date: '2026-09-13 04:30'
labels:
  - cli
  - ux
dependencies: []
references:
  - internal/cli/commands.go
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/man.go
documentation:
  - docs/cli-reference.md
priority: medium
type: enhancement
ordinal: 127000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Go command tree has executable examples, but much of its generated help is still structural rather than instructive. Usage lines omit required positionals and alternate forms, many option descriptions merely repeat their flag names or are blank, parser sentinel values appear as misleading defaults, and the top-level list is flat despite the size of the command surface. This makes both interactive discovery and one-shot agent onboarding unnecessarily error-prone.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every canonical command usage line shows required positional arguments, guarded command boundaries, and supported alternate forms, including exec, policy describe, op inspect, and both history projections.
- [x] #2 Every visible option has a concise operational description; no option has blank help or merely repeats its flag name.
- [x] #3 Help shows effective runtime defaults where meaningful and does not expose internal zero-value sentinels or contradictory defaults.
- [x] #4 Top-level help groups commands into scannable lifecycle, inspection/recovery, and administration sections while preserving parsing behavior.
- [x] #5 A deterministic aggregate help invocation covers every canonical top-level and nested command without duplicate sections and remains read-only.
- [x] #6 Rendered-help and generated-man tests protect grouping, usages, descriptions, defaults, aggregate coverage, examples, JSON error behavior, and ANSI-free redirected output.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace flag-name-only option usages with a shared description table so every visible flag has operational help; add DefaultText for effective runtime defaults (ttl 15m, poll-interval 250ms, max-duration 1h, limit 50, timeout 30s, retention-days 30, reason released, agent login user) and HideDefault for zero sentinels.
2. Set UsageText per command with required positionals and alternate forms (exec -- COMMAND, policy describe NAME, op inspect selectors, history projections, verify hook form, resource-input alternatives); explain the shared [selection] placeholder in each mutating command description.
3. Group top-level commands with urfave Category into Claim lifecycle, Inspection and recovery, and Setup and administration; man page adds a grouped COMMANDS section.
4. Add a custom help command with --all that walks the tree deterministically and prints every canonical command help once, read-only.
5. Extend root_test and man_test: no blank or name-only usages, no '(default: 0' sentinels, usage prefixes and positionals, categories, aggregate coverage, JSON error, ANSI-free redirected output. Update docs/cli-reference.md and CHANGELOG.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
commands.go now sources option help from one flagUsage table with backquoted placeholders (--resource KEY, --ttl DURATION), DefaultText for effective defaults (ttl 15m, poll-interval 250ms, max-duration 1h, limit 50, timeout 30s, retention-days 30, reason released, agent login user, coverage claim, client claude-code, scope project) and HideDefault for zero sentinels; validateCLICommandTree panics at startup on blank/name-only usage or '(default: 0'. UsageText per command shows required inputs and alternate forms (resource-input alternatives, exec -- COMMAND [ARGS...], policy describe NAME, op inspect selectors, both history projections, verify hook form, checkpoint data forms, watch forms) and a shared 'Selection:' paragraph explains [selection]. Commands carry Category values that sort into Claim lifecycle / Inspection and recovery / Setup and administration; the man page adds a grouped COMMANDS section and renders placeholders and defaults. A custom help command replaces urfave's: 'help --all' prints root help then every visible command once in DFS order, read-only; 'help op inspect' resolves nested paths; unknown names return the invalid-argument JSON envelope under --json. policy describe without NAME now lists available policies. Verified with TestEveryOptionHasOperationalHelpAndNoSentinelDefaults, TestUsageLinesShowPositionalsAndAlternateForms, TestRootHelpGroupsCommandsInWorkflowOrder, TestHelpAllCoversEveryCommandOnceReadOnly (determinism, no state dir created, no ANSI, one section per command), TestHelpCommandResolvesNestedPathsAndReportsUnknownInJSON, TestCanonicalCommandHelpPathsFlagsAndExamples, TestWriteManPageDerivesRegisteredCommandsFlagsExamplesAndVersion, plus mise run test/lint/typecheck/format-check, smoke, doc-test, man, e2e.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Go CLI help is now instructive: every option has operational help with placeholders and effective defaults, usage lines show positionals and alternate forms, top-level help and the manual group commands by workflow, and 'worklease help --all' prints the whole tree once. Verified by rendered-help and man tests, JSON error and ANSI-free checks, and the full quality gates; docs and changelog updated.
<!-- SECTION:FINAL_SUMMARY:END -->
