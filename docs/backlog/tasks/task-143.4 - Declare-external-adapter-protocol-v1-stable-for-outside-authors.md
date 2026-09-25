---
id: TASK-143.4
title: Declare external adapter protocol v1 stable for outside authors
status: Done
assignee: []
created_date: '2026-09-25 16:38'
updated_date: '2026-09-25 22:27'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies: []
documentation:
  - docs/external-adapter-protocol.md
  - docs/external-adapter/v1/schema.json
parent_task_id: TASK-143
priority: medium
type: feature
ordinal: 76000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The protocol document is written as the internal S7 wire contract. Outside authors need to know whether v1 is stable to build on, where the canonical schema lives, and how compatibility and deprecation work before they invest in an adapter.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The protocol document states v1's stability status, what may change compatibly within v1, and the deprecation process, referencing the existing compatibility policy rather than duplicating it.
- [x] #2 The JSON schema is published at a versioned, documented path with a stable `$id`, and the doc-test verifies the path and `$id` match.
- [x] #3 The manifest's protocol range and the host's supported majors are reported by a `--json` CLI command so tooling can check compatibility without starting an adapter.
- [x] #4 The authoring guide links the stable spec and schema as the entry point for new authors.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect protocol/schema, compatibility policy, CLI JSON patterns and doc-test conventions. 2. Publish a stable versioned schema and document v1 compatibility/deprecation; expose supported majors and manifest range through a read-only JSON CLI command. 3. Add focused tests and guide links; run race and repository gates. 4. Review, commit, merge to main, finalize the task and clean the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation in task-143-4-stable-protocol: stable v1 spec, versioned schema path/$id, doc-test link/identity check, offline queue adapter protocol JSON inspection of host majors and saved manifest range. Review identified false compatibility verdict and unknown-flag echo; corrected to protocolMajorsOverlap only with explicit negotiation caveat and a fixed invalid-argument error. Focused race count=3, lint, format-check, typecheck, doc-test pass; full test suite and staged hooks passed before corrections. Post-correction staged hooks hit an unrelated TestQueueNextStartOutcomes/mcp/unknown-readback 30s lease-expiry under concurrent suites; another checkout is investigating that test. Next: rerun staged hooks when competing suites finish, commit/merge to main, verify and close.

Completion evidence: AC1 reviewed docs/external-adapter-protocol.md Compatibility section (v1 stable; additive optional fields/capabilities, unknown required features refused, major/deprecation notice/fixtures); AC2 mise run doc-test parsed schema at docs/external-adapter/v1/schema.json, asserted stable $id and all three relative links; AC3 go test -race -count=3 TestQueueAdapterProtocolReportsStaticCompatibility covered host majors, manifest range, overlap, unknown required feature not reported as full compatibility, invalid/missing file and secret-looking flag redaction; AC4 guide entry links spec/schema and documents CLI flags/JSON/exit codes. Reviewer found two concrete defects, both corrected, affected tests rerun. Lint, format-check, test, typecheck, doc-test, staged hooks passed; post-merge main race and doc-test passed. Implementation commit bd4b9d1 rebased/fast-forward merged as 75a4f12 on main, preserving other staged and unstaged changes. No push or PR. Next resumable work TASK-143.5; no blocker for 143.4.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Declared external adapter protocol v1 stable, published a versioned schema, added offline JSON major-range inspection and author links. Verified focused race, full lint/format/test/typecheck, doc-test, hooks and post-merge checks; integrated as 75a4f12.
<!-- SECTION:FINAL_SUMMARY:END -->
