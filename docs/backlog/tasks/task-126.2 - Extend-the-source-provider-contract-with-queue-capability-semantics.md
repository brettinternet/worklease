---
id: TASK-126.2
title: Extend the source-provider contract with queue capability semantics
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies: []
references:
  - skills/worklease-workflow/references/source-provider-contract.md
  - skills/worklease-workflow/references/source-provider-authoring-checklist.md
  - skills/worklease-workflow/references/contract.md
  - skills/worklease-workflow/references/source-providers/backlog-md.md
  - skills/worklease-workflow/references/source-providers/github-issues.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-126
priority: high
type: docs
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The queue needs capability, pagination, coverage, freshness, principal, and side-effect semantics that the existing source-provider contract does not define (plan section 7, D15). The plan requires extending the existing contract rather than writing a second specification (plan section 4). Operation names stay illustrative, because this is a conceptual contract, not a wire protocol; the wire protocol is S7.

Keep the contract provider-neutral. Provider-specific facts belong in the provider references under skills/worklease-workflow/references/source-providers/. TASK-128.3 implements these semantics as Go types, so they must be precise enough to test.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 source-provider-contract.md defines a Capability value with support (supported|unsupported|unknown), permission (allowed|denied|unknown), availability (available|unavailable|authentication-required), semantics, limits, and reason. It states that unknown never means allowed, and that capabilities are evaluated at adapter, source, principal, and item/action scope
- [ ] #2 It defines paginated summary listing (cursor, coverage, and a total marked exact, estimated, or unknown), batched item reads with per-item outcomes and no hidden unbounded fan-out, dependency reads with completeness, and an optional change feed. Every response carries principal and configuration generation, observation time, coverage, and an opaque provider version when one exists
- [ ] #3 It states that claim revision, provider version, update timestamp, and sync cursor are distinct values, and that a read ETag or timestamp never implies a conditional-write capability
- [ ] #4 Dependency reads carry typed relationship evidence: relationship type, direction, source-qualified endpoints, observation or version, declared completion condition, and the configured interpretation next to the raw provider outcome (plan section 7, D27). Resolving a reference never authorizes a new source, endpoint, or credential scope
- [ ] #5 It lists the capability groups from plan section 7, including Effects (Git fetch, commit, hooks, watcher notifications) and Authentication, and states that read-only discovery never probes a capability by attempting a write
- [ ] #6 It defines structured diagnostics for unsupported capability, authentication, authorization, conflict, rate limiting with a retry time, unavailable source, incomplete graph, and unknown outcome, mapped to (not replacing) the result vocabulary in contract.md
- [ ] #7 source-provider-authoring-checklist.md gains a checklist item for each new obligation
- [ ] #8 source-providers/backlog-md.md and github-issues.md record the initial capability declarations from the plan section 7 table
- [ ] #9 contract.md gains no provider-specific assumptions, and every link from skills/worklease-workflow/SKILL.md still resolves
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run doc-test` passes and every changed relative link resolves
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
