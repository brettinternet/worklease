---
id: TASK-85.1
title: Ratify the Go product contract and inventory Python capabilities
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 04:07'
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
The Go Product Contract (`docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md`) fixes the design for the rewrite so implementing agents do not make product decisions. What it does not yet contain is a complete, evidence-backed inventory of what the Python proof of concept actually does. Without that inventory an implementer may silently drop a safety behavior the contract forgot, or port accidental complexity the contract meant to remove.

This is a bounded documentation spike, not a design task. Fixed decisions in contract section 2 are not up for revision here. If the inventory reveals a contradiction, record it as a comment on TASK-85 and as a proposed amendment (contract section 15); do not change the decision.

Work:

1. Walk every Python surface: each `add_parser` command in `src/worklease/cli.py` (25 commands), each MCP tool in `src/worklease/mcp_server.py` (7 tools), each module in `src/worklease/` and `src/worklease/adapters/`, each file in `scripts/`, each page in `docs/*.md`, and the `skills/` tree.
2. For each surface write one inventory row with these columns: Python surface | one-line behavior | disposition (`retain`, `redesign`, `remove`, `defer`) | contract section specifying the Go behavior | owning task (TASK-85.2 to TASK-85.18) | Python test names that are the edge-case evidence (from `tests/`).
3. Create the inventory with `backlog doc create "Go Rewrite Capability Inventory" -p go-rewrite -t specification`, then fill it with `backlog doc update <id> --content "$(cat file)"`. Never edit the Markdown file directly.
4. Where the contract is silent about a Python behavior worth keeping (a safety check, a redaction rule, an edge case with a test), add the row with disposition `retain` and write the exact proposed contract sentence in a "Gaps" section of the inventory.
5. List every Python test function name under a "Not carried forward" heading when it is Python-only (packaging, entry points, SDK) so later tasks know it was considered.
6. Add one comment on TASK-85 summarizing the gaps (or stating there are none).

Owned paths: the new inventory document under `docs/backlog/docs/go-rewrite/` only. No code changes. Out of scope: editing the contract's fixed decisions, creating or editing other tasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A backlog document titled "Go Rewrite Capability Inventory" exists under docs/backlog/docs/go-rewrite/ with one row per Python CLI command (all 25 add_parser entries in src/worklease/cli.py), per MCP tool (7), per module in src/worklease/ and src/worklease/adapters/, per file in scripts/, and per page in docs/*.md; every row has disposition, contract section, owning task, and evidence columns filled.
- [ ] #2 Every retain or redesign row names exactly one owning task from TASK-85.2 to TASK-85.18 and a contract section number that specifies the Go behavior; every remove row cites contract section 16 or gives a one-line rationale; every defer row names the follow-up decision the owner must make.
- [ ] #3 Every test function in tests/*.py appears in the evidence column of at least one row or under the "Not carried forward" heading with a reason.
- [ ] #4 A "Gaps" section lists every retained Python behavior the contract does not specify, each with the exact proposed contract sentence and the task that should implement it, and TASK-85 carries one comment summarizing the gaps or stating there are none.
- [ ] #5 The contract document is unchanged by this task: `git diff -- "docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md"` is empty at finalization.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Inventory document created and updated only through the backlog CLI
<!-- DOD:END -->
