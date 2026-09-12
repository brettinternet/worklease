---
id: TASK-85.5
title: Implement resource policies and key derivation
status: Done
assignee:
  - '@pi-01a09478'
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 07:57'
labels:
  - go-rewrite
milestone: m-0
dependencies:
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
modified_files:
  - CHANGELOG.md
  - internal/cli/commands.go
  - internal/cli/resource_commands.go
  - internal/cli/resource_commands_test.go
  - internal/cli/resource_input.go
  - internal/cli/selection.go
  - internal/resource/resource.go
  - internal/resource/resource_test.go
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement deterministic static resource policies, key/policy commands and shared acquire resource input. Read contract sections 4, 7.1, 7.11, 7.13 and 20. Own internal/resource and key/policy/resource-input CLI files. Reuse the cited Python derivation evidence without carrying plugins or fencing claims forward.

Identities are exact opaque bytes. Encode tuple components unambiguously, resolve canonical path aliases including missing leaves, and distinguish host-local filesystem keys from portable provider keys. Path resources cover exact files only; task/source identity does not implicitly protect arbitrary edits. No cross-host repository namespace configuration is introduced now.

Evidence and patterns (the amended contract is normative): `src/worklease/adapters/protocol.py` (local_resource, coordination_resource, normalize_provider, require_identity, and the GIT_* stripping in _git_output), `backlog_md.py`, `markdown.py`, `github.py`, `linear.py`, `registry.py` (the generic policy). Tests: `tests/test_adapters.py` test_github_key_matches_reference_policy, test_backlog_and_markdown_use_repository_local_identity, test_missing_git_uses_path_fallback, test_nested_source_keys_match_across_linked_worktrees, test_generic_policy_is_explicit_and_unknown_names_fail, test_source_scope_collision_checks_every_sample_item. TASK-84 (closed as superseded) records the `path` policy intent. Pattern: hum `internal/config/config.go` for validation error wording.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Golden derivation tests cover every static policy, delimiter-containing source/item values without collisions, linked-worktree agreement, and separation of unrelated repositories.
- [x] #2 Path tests canonicalize symlinks and missing leaves through existing ancestors, reject escapes/non-Git paths and invalid traversal, and explicitly show no parent/child/glob overlap.
- [x] #3 Validation rejects unknown policies, blank/control/oversized identities, duplicate resource inputs and mixed resource-input modes before mutation; the registry is deterministic with unique names.
- [x] #4 Key and policy text/JSON report resource, provider, capability, scope, identityScope, localReplaceAllowed and providerFencing:false; commands make no network calls.
- [x] #5 Coordination-only input disables local replacement capability without claiming provider fencing; acquire can reuse the input resolver and mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect the Go CLI/config contracts and Python adapter derivation behavior.
2. Implement deterministic resource policies, canonical path handling, and shared resource-input validation.
3. Add key/policy CLI projections and focused golden/validation tests.
4. Run ci-go and repository quality gates, review the diff, and record evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed with Worklease resource backlog-md:/Users/brett/dev/me/worklease/.git:docs/backlog:TASK-85.5. Guarantee: local coordination among cooperating callers on this host; provider writes are not fenced.

Implemented and merged commit f0dd2a2. Review found identity-normalization, cross-repository symlink, working-directory, Git-output whitespace, descriptor, and test-evidence defects; all were fixed before merge. Validation passed: mise run ci-go; mise run lint; mise run format-check; mise run test (339 Python tests); mise run typecheck. go list dependency inspection found no net or net/http dependency in the resource or CLI packages.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented six static Go resource policies and shared CLI resource-input resolution. AC1: TestStaticPolicyGoldenDerivations, TestRFC3986EncodingAndCanonicalCoordinationKey, and TestLinkedWorktreesAndSymlinkMissingLeaf prove golden, collision, and repository identity behavior. AC2: TestPathCanonicalContainmentAndExactSemantics, TestLinkedWorktreesAndSymlinkMissingLeaf, and TestPathPreservesGitRootWhitespace prove canonicalization, containment, and exact-path behavior. AC3: TestValidationAndDeterministicRegistry and TestResourceInputResolverRejectsDuplicateAndMixedModesBeforeAcquire prove validation and deterministic registration. AC4: TestKeyAndPolicyCommandsReportContractMetadata proves text and JSON projections; go list dependency inspection confirms no network dependency. AC5: TestKeyAndPolicyCommandsReportContractMetadata, TestAcquireDerivesInputBeforeLaterPlaceholder, and mise run ci-go on f0dd2a2 prove coordination-only semantics, shared acquire resolution, and the final Go gate.
<!-- SECTION:FINAL_SUMMARY:END -->
