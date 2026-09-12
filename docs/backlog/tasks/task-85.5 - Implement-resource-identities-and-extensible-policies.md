---
id: TASK-85.5
title: Implement resource policies and key derivation
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 05:56'
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
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement deterministic static resource policies, key/policy commands and shared acquire resource input. Read contract sections 4, 7.1, 7.11, 7.13 and 20. Own internal/resource and key/policy/resource-input CLI files. Reuse the cited Python derivation evidence without carrying plugins or fencing claims forward.

Identities are exact opaque bytes. Encode tuple components unambiguously, resolve canonical path aliases including missing leaves, and distinguish host-local filesystem keys from portable provider keys. Path resources cover exact files only; task/source identity does not implicitly protect arbitrary edits. No cross-host repository namespace configuration is introduced now.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Golden derivation tests cover every static policy, delimiter-containing source/item values without collisions, linked-worktree agreement, and separation of unrelated repositories.
- [ ] #2 Path tests canonicalize symlinks and missing leaves through existing ancestors, reject escapes/non-Git paths and invalid traversal, and explicitly show no parent/child/glob overlap.
- [ ] #3 Validation rejects unknown policies, blank/control/oversized identities, duplicate resource inputs and mixed resource-input modes before mutation; the registry is deterministic with unique names.
- [ ] #4 Key and policy text/JSON report resource, provider, capability, scope, identityScope, localReplaceAllowed and providerFencing:false; commands make no network calls.
- [ ] #5 Coordination-only input disables local replacement capability without claiming provider fencing; acquire can reuse the input resolver and mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
