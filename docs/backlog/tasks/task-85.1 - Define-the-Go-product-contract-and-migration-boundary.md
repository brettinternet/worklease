---
id: TASK-85.1
title: Inventory capabilities and safety evidence for the Go rewrite
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 06:27'
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
- [ ] #1 An evidence-backed Go Rewrite Capability Inventory records HEAD and covers discovered CLI/MCP capabilities plus modules, scripts, docs, skills and SDK by capability family; counts are derived from the recorded checkout.
- [ ] #2 Each retained/redesigned capability names one primary TASK-85.x owner, applicable contract sections, and representative named safety tests retrievable with git show at the inventory SHA.
- [ ] #3 Intentional removals and changed semantics are explicit; there is no Python API/schema/output/test parity requirement, and the remote design is marked deferred rather than removed.
- [ ] #4 Material safety gaps are resolved in the normative contract through section 15 and synchronized with owning tasks; TASK-85 has a comment summarizing the result.
- [ ] #5 Backlog integrity and available repository quality gates pass; the inventory is committed and TASK-85.2 depends on this completed prerequisite.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Inventory document created and updated only through the backlog CLI
<!-- DOD:END -->
