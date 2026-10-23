---
id: TASK-32
title: Compact human-readable list output
status: done
type: enhancement
priority: medium
assignees: ['@codex-loop-fresh-20260715-worklease-main']
labels: [cli, ux]
rank: a3a62ce98b3a62cb6
refs: [src/worklease/cli.py, tests/test_cli.py, README.md]
created: 2026-07-15T22:22:00Z
updated: 2026-07-15T22:35:00Z
---

Make the default human-readable `list` table easier to scan when resource paths, claim IDs, owner IDs, and expiry timestamps are long. Preserve exact values for automation and provide an explicit full display option for operators who need the unabridged text representation.

## Acceptance criteria

- [x] Default text list output uses bounded, deterministic compact renderings for resource values and long identifiers without changing the underlying claim payload.
- [x] Default text list expiry values are short relative durations that clearly distinguish active time remaining from expired elapsed time.
- [x] `worklease list --full` shows the full resource, claim ID, owner ID, and absolute expiry timestamp; explicit JSON output remains unchanged and complete.
- [x] CLI contract tests and human-readable output documentation cover compact and full list modes, including long values and expired claims.

## Plan

1. Add a list-only --full option and bounded deterministic text formatters for resources, identifiers, and expiry values while leaving JSON payloads untouched. 2. Propagate the display mode through CLI rendering and cover fixed-clock compact, expired, full, and explicit JSON behavior in CLI contract tests. 3. Document the compact default and full list display in the human-readable grammar.

## Notes

Implemented list-only --full rendering and compact default text: bounded prefix/suffix values, relative active/expired expiry display, and raw JSON preservation. Added fixed-clock formatter tests plus CLI full/JSON coverage and documented the grammar in README.md. Focused tests and list --help smoke check pass.

Final validation: fixed-clock formatter coverage proves bounded prefix/suffix shortening and active/expired relative rendering; CLI smoke output proves default compact text, --full absolute text, and --json complete values. mise run lint, mise run format-check, mise run test, mise run typecheck, and mise run hooks all passed.

## Summary

Implemented compact human-readable list output. Default text now bounds resource and identifier columns, uses relative active/expired expiry values, and keeps table alignment; work-source values remain opaque and are shortened generically. Added list-only --full for unabridged text, preserved complete JSON output, added deterministic formatter and CLI contract coverage, and documented the grammar in README.md. Verified with focused list tests, direct compact/full/JSON smoke output, list --help, mise lint, format-check, test, typecheck, and hooks.
