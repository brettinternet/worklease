---
id: TASK-85.1
title: Inventory capabilities and safety evidence for the Go rewrite
status: Done
assignee:
  - '@pi-01a09450'
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 06:41'
labels:
  - go-rewrite
milestone: m-0
dependencies: []
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/cli.py
  - src/worklease/mcp_server.py
  - src/worklease/adapters
  - tests
  - docs/cli-reference.md
  - docs/claim-model.md
  - docs/mcp.md
documentation:
  - doc-3 - Go Rewrite Capability Inventory
modified_files:
  - docs/backlog/docs/go-rewrite/doc-3 - Go-Rewrite-Capability-Inventory.md
  - >-
    docs/backlog/tasks/task-85.1 -
    Define-the-Go-product-contract-and-migration-boundary.md
parent_task_id: TASK-85
priority: high
type: spike
ordinal: 93000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Inventory the useful Python capabilities and safety evidence before Go implementation begins. The owner explicitly permits redesign with no Python compatibility. The amended Go Product Contract is normative; Python behavior is evidence, not a parity checklist.

Record the current HEAD SHA so paths remain retrievable after retirement. Discover current CLI/MCP surfaces instead of hard-coding counts (TASK-86 observed 26 add_parser calls and seven Python MCP tools). Group modules, scripts, docs, skills and SDK surfaces by capability; give each retained/redesigned capability an owning Go task, contract section, and representative named safety tests. Identify intentional removals and the deferred remote proposal. Do not enumerate every Python test or require one-to-one test/module ports.

Create the Go Rewrite Capability Inventory through backlog doc create/update. Record concrete missing safety behaviors and any proposed amendments, or explicitly state none. Resolve material gaps through section 15 before declaring this prerequisite complete. No implementation work is included. Owned surface: inventory document and a TASK-85 summary comment; current TASK-86 amendments need not be re-ratified.

Evidence and patterns (the amended contract is normative; Python is behavior evidence, not a parity target): CLI surfaces are the `add_parser` calls in `src/worklease/cli.py`; MCP tools are in `src/worklease/mcp_server.py`; identity policies in `src/worklease/adapters/`; release and packaging in `scripts/*.py`; prior human docs in `docs/cli-reference.md`, `docs/claim-model.md`, `docs/mcp.md`; the reusable skill under `skills/worklease-workflow/`. Named safety test families to cite: `tests/test_store.py` (concurrency, expiry, transfer, idempotency, clock), `tests/test_execution.py` (process groups, pipes, timeouts, replacement), `tests/test_credentials.py` and `tests/test_lease_context.py` (secret and handle safety), `tests/test_gc.py` and `tests/test_history.py` (retention, redaction, cursors), `tests/test_mcp.py` (heartbeat, handles, EOF).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 An evidence-backed Go Rewrite Capability Inventory records HEAD and covers discovered CLI/MCP capabilities plus modules, scripts, docs, skills and SDK by capability family; counts are derived from the recorded checkout.
- [x] #2 Each retained/redesigned capability names one primary TASK-85.x owner, applicable contract sections, and representative named safety tests retrievable with git show at the inventory SHA.
- [x] #3 Intentional removals and changed semantics are explicit; there is no Python API/schema/output/test parity requirement, and the remote design is marked deferred rather than removed.
- [x] #4 Material safety gaps are resolved in the normative contract through section 15 and synchronized with owning tasks; TASK-85 has a comment summarizing the result.
- [x] #5 Backlog integrity and available repository quality gates pass; the inventory is committed and TASK-85.2 depends on this completed prerequisite.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 Inventory document created and updated only through the backlog CLI
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Record the current commit and inventory the normative contract, discovered CLI/MCP surfaces, Python capability families, release/docs/skill/SDK artifacts, and representative safety tests.
2. Create the Go Rewrite Capability Inventory through the backlog CLI, mapping retained or redesigned capabilities to one TASK-85.x owner and contract sections while documenting removals, semantic changes, deferred remote authority, and any contract gaps.
3. Add the TASK-85 summary comment, run backlog integrity and repository quality gates, review the generated diff, and record objective completion evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Created doc-3 through the Backlog CLI. Inventory evidence at commit 6a92441 covers 26 CLI parser registrations, seven Python MCP tools, all capability families, one primary Go owner per row, named safety tests, intentional removals, changed semantics, and deferred remote authority. No material contract gap or section 15 amendment was identified.

Validation passed: mise run lint, mise run format-check, mise run test (339 tests), mise run typecheck (zero errors), mise run hooks, backlog doctor, and git diff --check. A source check counted 26 add_parser calls and seven Python MCP tools; an AST check found all 63 named test functions in the recorded checkout. Independent inventory scouting found no additional contract gap. The verifier process could not start because its installed native extension was corrupt, so focused source/AST checks and the full repository gates provide acceptance evidence.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Created and committed doc-3, the evidence-backed Go Rewrite Capability Inventory. It maps the discovered CLI, MCP, module, SDK, documentation, skill, and release surfaces to one primary Go owner per family, records intentional incompatibilities and deferred remote authority, and finds no new section 15 amendment. Verified with source/AST checks, all repository quality gates, 339 tests, type checking, hooks, and Backlog integrity; TASK-85.2 retains its dependency on this completed prerequisite.
<!-- SECTION:FINAL_SUMMARY:END -->
