---
id: TASK-133.1
title: Specify the external adapter protocol and manifest
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-132
references:
  - skills/worklease-workflow/references/source-provider-contract.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-133
priority: medium
type: docs
ordinal: 43000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Derive the protocol from the two working built-in adapters rather than designing it ahead of them (plan sections 3 and 11). Plan section 11 lists what the boundary needs: a manifest (identity, version, protocol range, configuration schema, authentication methods, resource policy selection, declared capabilities), request IDs, cancellation, deadlines, bounded message and collection sizes, pagination, backpressure, protocol on stdout with bounded redacted diagnostics on stderr, and source-scoped credential references. MCP tools are not the protocol, because they carry no standard pagination, capability, coverage, or receipt semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A protocol document under docs/ specifies JSON-RPC 2.0 framing over stdio, the method set mirroring the TASK-126.2 conceptual operations, and error codes mapped to the TASK-126.2 diagnostics
- [ ] #2 It specifies the manifest schema, major-version negotiation, and the rule that unknown optional fields may be ignored while unknown required semantics may not
- [ ] #3 It specifies request IDs, cancellation, deadlines, maximum message and collection sizes, pagination, backpressure, and the stderr diagnostic limits
- [ ] #4 It specifies how credentials reach an adapter (source-scoped references or a narrow helper channel), states that Worklease bearer credentials are never sent, and states the minimized inherited environment
- [ ] #5 It states the trust model: process isolation is not a sandbox, and an installed adapter runs with the user's privileges
- [ ] #6 It includes a compatibility and deprecation policy, and a JSON Schema for every message is committed for use by TASK-133.2 and TASK-133.3
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run doc-test` passes and every changed relative link resolves
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
