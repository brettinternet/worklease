---
id: TASK-76
title: Accept provider key inputs on acquire
status: To Do
assignee: []
created_date: '2026-09-12 02:18'
labels:
  - cli
  - ux
dependencies:
  - TASK-67.3
references:
  - src/worklease/cli.py
  - src/worklease/cli_dispatch.py
  - src/worklease/adapters/__init__.py
  - tests/test_cli.py
  - README.md
  - docs/cli-reference.md
  - skills/worklease-workflow/SKILL.md
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 83000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deriving a provider-backed resource is a two-step pipe: `worklease --json key ... | python3 -c 'json.load(...)["resource"]'` and then `acquire -r "$RESOURCE"`. Every README and skill example carries that pipe and a Python dependency just to move one string between two Worklease commands. After TASK-67, `acquire` is the only lifecycle command that still needs the resource, so accepting the key inputs there removes the step everywhere.

## Decision

`acquire` gains `-p/--provider`, `--source`, and `-i/--item` with the same names, short options, and help as `key`. The three are required together and are mutually exclusive with `-r/--resource`. The resource is derived through `adapters.key_result` exactly as `key` does, and `-C/--coordination-only` is passed to that derivation as `key -C` passes it while still marking the epoch as it does today. No new payload fields: the derived resource appears in the claim as usual. `acquire-bundle` is out of scope because bundles take several resources. `key` remains for callers who need the full key result.

This task depends on TASK-67.3 so the README and skill examples it rewrites are the final path-free ones.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease acquire -p backlog-md --source docs/backlog -i TASK-42` acquires a resource whose identity is byte-identical to the `resource` from `worklease key` with the same inputs, including when `-C` is supplied to both.
- [ ] #2 Supplying only one or two of `--provider`, `--source`, and `--item`, or any of them together with `-r`, exits 64 through the standard error envelope with an actionable hint before the store is opened.
- [ ] #3 An unknown provider or invalid item fails with the same error reasons and exit codes `key` emits for those inputs.
- [ ] #4 The `acquire` help and epilog show the key-input form; the README lifecycle and the skill loop example no longer pipe `key` output through Python; `docs/cli-reference.md` lists `-p` and `-i` on `acquire` in the short option table; CHANGELOG `Unreleased` is updated.
- [ ] #5 Tests cover successful derivation for a provider key, `-C` parity with `key`, each partial and conflicting combination, and provider errors, and `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
