---
id: TASK-85.5
title: Implement resource policies and key derivation
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.2
  - TASK-85.3
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/adapters/protocol.py
  - src/worklease/adapters/backlog_md.py
  - src/worklease/adapters/markdown.py
  - src/worklease/adapters/github.py
  - src/worklease/adapters/linear.py
  - src/worklease/adapters/registry.py
  - tests/test_adapters.py
  - TASK-84
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Contenders can only coordinate if they derive byte-identical resources from the same logical work item. The Python adapters got this right for Backlog.md, Markdown, GitHub, and Linear but wrapped it in entry-point plugin loading and an SDK nobody uses. This task ports the deterministic derivations exactly as fixed in contract section 7.13 (including the new `path` policy from TASK-84) into a static Go registry, and delivers the `key` and `policy` commands plus the shared resource-input flag group that `acquire` uses.

Read first: contract sections 4 (`key`, `policy`, and the resource input modes), 6, 7.1, 7.11, 7.13, and 18 (resource sketch). Python evidence: `src/worklease/adapters/protocol.py` (local_resource, coordination_resource, normalize_provider, require_identity, the GIT_* stripping in _git_output), `adapters/backlog_md.py`, `adapters/markdown.py`, `adapters/github.py`, `adapters/linear.py`, `adapters/registry.py` (the generic policy), and in `tests/test_adapters.py`: test_github_key_matches_reference_policy, test_backlog_and_markdown_use_repository_local_identity, test_missing_git_uses_path_fallback, test_nested_source_keys_match_across_linked_worktrees, test_generic_policy_is_explicit_and_unknown_names_fail, test_source_scope_collision_checks_every_sample_item. TASK-84 describes the `path` policy intent. Pattern: `../hum/internal/config/config.go` for validation error wording.

Deliver in `internal/resource`: the `Policy` interface and `Descriptor`; a static registry map with `Lookup` and `Names`; the six policies with derivations byte-for-byte as in contract 7.13; an exported Git probe helper (runs `git` with `GIT_*` removed and returns a nil result on failure) that later tasks reuse; `ValidateResource` implementing contract 7.1; `RunConformance(t, policy)` asserting determinism, stability across working directories, scope collision behavior, and rejection of blank inputs, run against every registered policy. Deliver in `internal/cli`: `key`, `policy list`, `policy describe NAME`, and a shared `ResourceInput` flag group (repeated `-r`, provider triple, `--path`) resolving to an ordered resource list plus optional `Key`, ready for `acquire` in TASK-85.7.

Owned paths: `internal/resource`, `internal/cli/key.go`, `internal/cli/policy.go`, `internal/cli/resource_input.go` and their tests. Out of scope: acquiring claims; provider network calls; plugins or entry points.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Golden tests fix each derivation: backlog-md and markdown produce <provider>:<common>:<locator>:<item or __source__> with the same value from the main worktree and a linked worktree and a different value for a second repository; github lowercases and strips .git; linear and generic produce coordination:<provider>:<sha256> of canonical JSON; path produces path:<common>:<relative> for a file and for a directory, identical across linked worktrees.
- [ ] #2 Rejections are tested: unknown policy names fail unknown-policy listing available names; blank source or item, control characters, over-length values, and leading or trailing whitespace fail invalid-resource; path outside the repository, with .. traversal, or in a non-Git directory fails invalid-path; supplying more than one resource input mode fails resource-input-conflict.
- [ ] #3 RunConformance passes for every registered policy, and a registry validation test fails if two policies share a name.
- [ ] #4 `worklease key`, `policy list`, and `policy describe` work in text and JSON with the fields named in contract 7.13, `policy list --full` shows capability and scope columns, and a test asserts key executes nothing other than git rev-parse.
- [ ] #5 `--coordination-only` on key reports capability local-coordination and fencedMutations false for otherwise fenced policies, and `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
