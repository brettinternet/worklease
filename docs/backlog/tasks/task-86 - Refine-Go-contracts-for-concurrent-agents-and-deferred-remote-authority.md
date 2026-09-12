---
id: TASK-86
title: Refine Go contracts for concurrent agents and deferred remote authority
status: Done
assignee:
  - '@codex'
created_date: '2026-09-12 05:32'
updated_date: '2026-09-12 06:10'
labels:
  - contract-review
dependencies: []
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - docs/distributed-cloudflare-claim-authority.md
priority: high
type: docs
ordinal: 111000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Owner-requested product and backlog review before the Go rewrite. There are no Python compatibility constraints. Resolve contract contradictions that affect concurrent agent loops, retain a bounded path to a future remote authority, and postpone all remote implementation. This review is distinct from TASK-85.1, which inventories Python capability evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The Go contract defines coherent ownership, guarded-operation, credential-recovery, and multi-agent handle boundaries with documented review counterexamples.
- [x] #2 Existing Go task descriptions, acceptance criteria, and dependencies agree with the amended contract, and the capability inventory is a prerequisite for implementation.
- [x] #3 The Cloudflare design remains a deferred Go-oriented proposal with explicit current seams and deferred remote decisions; no remote feature or Go implementation is added.
- [x] #4 Backlog integrity and required repository quality gates pass, the refinement is independently reviewed, and changes are committed.
- [x] #5 The owner-requested human CLI and MCP/JSON ergonomics priorities are normative, mapped to executable acceptance journeys in existing Go tasks, and do not expand the deferred remote or eleven-tool MCP scope.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Read all contract sections and Go rewrite tasks; inspect Python evidence and remote proposal.
2. Amend the normative contract and matching existing tasks through Backlog.md; preserve deferred remote design.
3. Review adversarial concurrency and recovery scenarios, run repository gates and backlog integrity checks, record evidence, and commit.

Owner follow-up: make setup-free human commands and low-plumbing MCP/JSON orchestration first-class contract requirements; extend the existing CLI, MCP, setup and documentation task acceptance journeys, verify consistency and repository gates, then commit the focused amendment.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Reviewed from baseline e23c01721afe565c7e0fed09089cf9de8c1657c6. Read every Go contract section, all 18 children and parent, generic workflow contract, Python capability evidence and remote proposal. TASK-86 is documentation/backlog refinement only; Go implementation remains To Do.

Evidence: section 21 records concrete concurrency/recovery counterexamples and owners. Independent reviewer found partial predecessor recovery, MCP initial maxHold, history GC gaps, durable reconciliation retry and definitive reconciliation-failure cleanup issues; the contract and owning criteria now address each. Final recovery branches were manually checked: definitive failure clears recoveryRequest only, unknown retains both requests, confirmed reconciliation clears both after read-back.

Backlog doctor reports no duplicates, self dependencies or cycles. A structured task-view JSON graph check proves all 17 Go implementation tasks depend transitively on 85.1, and MCP depends on the instructions implementation. Task IDs, references, statuses and unchecked implementation criteria were preserved; redundant dependencies were removed.

Required gates passed: mise run lint; mise run format-check (203 files); mise run test (339 core + 19 SDK tests); mise run typecheck (both projects clean). mise run hooks-install succeeded with the required .git permission. git diff --check passes. Temporary mise/uv caches were used because the sandbox prevents the default cache writes.

Current official sources verified: Go downloads list Go 1.27.1; MCP 2026-07-28 versioning/stdio and 2025-11-25 legacy lifecycle; Cloudflare Durable Object transaction/interleaving guidance. The document links those sources. The Cloudflare file is retained as a Go-oriented deferred proposal; no remote code, Go code, provider action or release publication was added.

Owner ergonomics follow-up: manually checked section 1.1 against command selection/output, handles, MCP and setup sections. Mapped human/JSON and session journeys to 85.14 #6, minimal MCP lifecycle and structured failures to 85.15 #6, optional onboarding to 85.16 #6, runnable quick starts to 85.17 #6, and release closure to 85 #6. Read-back/scope checks prove the eleven-tool table and remote boundary are unchanged; implementation tasks remain To Do with unchecked criteria. These are planned Go acceptance journeys, not implemented behavior.

Follow-up verification: Backlog doctor and git diff --check pass; lint, format-check (203 files), typecheck (both projects), and the full test suite (339 core + 19 SDK) pass. The first test run found a missing editables wheel file in the shared temporary uv cache; rerunning the complete suite with a fresh isolated cache passed without changing code or weakening checks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Refined the Go product contract and all nineteen rewrite task records for concurrent agent loops, exact credential/request recovery, honest operation guarantees, and future authority identity. Preserved the Cloudflare proposal as deferred Go design. Independent review findings were resolved and mapped to implementation acceptance criteria. Verified Backlog integrity/dependency ordering, lint, formatting, type checks, 339 core tests and 19 SDK tests. Changes committed with the required Git hook enabled; no Go or remote feature was implemented.

Owner follow-up makes short setup-free human commands and low-plumbing JSON/MCP orchestration explicit release requirements, preserves structured MCP failure details, and adds focused executable acceptance journeys to existing tasks. Verified the unchanged tool/remote scope, Backlog integrity, all repository gates and 358 tests; Go implementation criteria remain unchecked.
<!-- SECTION:FINAL_SUMMARY:END -->
