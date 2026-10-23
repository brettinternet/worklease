---
id: TASK-25
title: Improve CLI help discoverability
status: done
type: enhancement
priority: medium
assignees: ['@codex-loop-fresh-20260714-worklease-pass']
labels: [cli, ux]
rank: a2dcf7ea712dcf7c2
created: 2026-07-15T04:10:00Z
updated: 2026-07-15T04:27:00Z
---

Make the worklease help menus easier to scan and more useful for common workflows by organizing commands and showing concise examples.

## Acceptance criteria

- [x] Top-level help groups related commands, including singleton, bundle, inspection/reconciliation, and maintenance commands.
- [x] Top-level help includes one or two safe, copyable common-usage examples.
- [x] The acquire, exec, release, and replace-file help menus each include a relevant example.
- [x] Subprocess tests verify each enriched help command exits successfully and prints its examples to stdout.
- [x] Existing command parsing and output behavior remain unchanged.

## Plan

1. Rework argparse help rendering into stable command groups while preserving aliases and parsing behavior. 2. Add concise safe examples to top-level help and acquire, exec, release, and replace-file help. 3. Add subprocess coverage for enriched help output and run the repository quality gates. 4. Verify acceptance criteria, commit the feature, and integrate the branch into local main.

## Notes

Implemented grouped top-level argparse help with Singleton, Bundles, Inspection and reconciliation, and Maintenance sections. Added concise top-level examples plus acquire, exec, release, and replace-file epilogs. Added subprocess coverage for grouped help and each command example. Smoke-tested all enriched help commands; mise run lint, format-check, typecheck, and test pass.

Independent verifier initially found a missing closing quote in the release example; changed the example to shell-safe single quotes and strengthened the test to assert each example tail. Re-ran targeted help tests and all quality gates successfully.

## Summary

Grouped top-level help into Singleton, Bundles, Inspection and reconciliation, and Maintenance sections while preserving command aliases. Added two top-level examples and shell-safe examples for acquire, exec, release, and replace-file. Added subprocess assertions for help exit status, grouped output, and complete example tails. Verified with targeted help subprocesses, sh -n validation of all four examples, mise run lint, mise run format-check, mise run typecheck, mise run test, and mise run hooks. Independent verifier passed all five acceptance criteria.
