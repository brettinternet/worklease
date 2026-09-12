---
id: TASK-76
title: Accept provider key inputs on acquire
status: To Do
assignee: []
created_date: '2026-09-12 02:18'
updated_date: '2026-09-12 02:24'
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
  - scripts/release_docs.py
priority: medium
type: enhancement
ordinal: 83000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Deriving a provider-backed resource is a two-step pipe: `worklease --json key ... | python3 -c ...` and then `acquire -r "$RESOURCE"`. Every README and skill example carries that pipe and a Python dependency just to move one string between two Worklease commands. After TASK-67, `acquire` is the only lifecycle command that still needs the resource, so accepting the key inputs there removes the step everywhere.

## Decision

`acquire` gains `-p/--provider`, `--source`, and `-i/--item` with the same names, short options, and help as `key`. The three form one complete input mode: all are required together, and the mode is mutually exclusive with `-r/--resource`. Validate the mode after parsing but before lifecycle defaults, lease-handle checks, or store construction, so invalid combinations return the standard `invalid-arguments` envelope without touching durable state.

For the provider mode, call `adapters.key_result` once with the same inputs as `key`, set the acquire resource from its `resource`, and set the epoch coordination flag from the returned capability (`not fencedMutations`). This preserves `-C` parity while also preventing inherently coordination-only policies such as Linear from recording a falsely fenced epoch when `-C` was omitted. The derived resource then participates in the existing work-key default, wait loop, lease-file flow, and claim payload exactly like an explicit resource. Do not add provider fields to the acquire response. `acquire-bundle` remains out of scope because bundles take several resources, and `key` remains for callers that need the full key result.

This task depends on TASK-67.3 so the README and skill examples it rewrites are the final path-free ones.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease acquire -p backlog-md --source docs/backlog -i TASK-42` acquires the byte-identical `resource` returned by `worklease key` for the same inputs, and the derived resource is used by the existing default work key, wait, and lease-file behavior.
- [ ] #2 The claim records `coordinationOnly` as `not fencedMutations` from the derived key: explicit `-C` has parity with `key -C`, while an inherently local-coordination policy such as Linear records a coordination-only epoch even without `-C`.
- [ ] #3 Supplying only one or two key inputs, or combining `-r` with any key input, exits 64 through the standard `invalid-arguments` envelope with an actionable hint before key derivation, lease-file access, or store construction.
- [ ] #4 Unknown providers and invalid provider identities fail with the same reason, details, and exit code as `key`, before store construction; no provider or key metadata is added to successful acquire output.
- [ ] #5 The `acquire` help and epilog show both input modes; the README lifecycle and workflow-skill loop no longer pipe `key` output through Python; `docs/cli-reference.md` lists `-p` and `-i` on `acquire`; CHANGELOG `Unreleased` is updated; generated release documentation renders successfully.
- [ ] #6 Tests cover fenced and inherently coordination-only policies, explicit `-C`, work-key defaulting, wait and lease-file compatibility, every partial and conflicting input shape, provider errors, no-store failure ordering, and unchanged acquire response shape; `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
