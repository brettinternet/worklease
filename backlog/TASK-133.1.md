---
id: TASK-133.1
title: Specify the external adapter protocol and manifest
status: done
type: docs
priority: medium
assignees: ['@pi']
labels: [work-queue, reviewed]
milestone: m-1
parent: TASK-133
blocked_by: [TASK-132]
rank: a4d3fc68324d3fc24
refs: [skills/worklease-workflow/references/source-provider-contract.md, docs/work-queue-tui-proposal.md]
created: 2026-09-23T04:29:00Z
updated: 2026-09-25T22:55:00Z
---

Derive the protocol from the two working built-in adapters rather than designing it ahead of them (plan sections 3 and 11). Plan section 11 lists what the boundary needs: a manifest (identity, version, protocol range, configuration schema, authentication methods, resource policy selection, declared capabilities), request IDs, cancellation, deadlines, bounded message and collection sizes, pagination, backpressure, protocol on stdout with bounded redacted diagnostics on stderr, and source-scoped credential references. MCP tools are not the protocol, because they carry no standard pagination, capability, coverage, or receipt semantics.

## Acceptance criteria

- [x] A protocol document under docs/ specifies JSON-RPC 2.0 framing over stdio, the method set mirroring the TASK-126.2 conceptual operations, and error codes mapped to the TASK-126.2 diagnostics
- [x] It specifies the manifest schema, major-version negotiation, and the rule that unknown optional fields may be ignored while unknown required semantics may not
- [x] It specifies request IDs, cancellation, deadlines, maximum message and collection sizes, pagination, backpressure, and the stderr diagnostic limits
- [x] It specifies how credentials reach an adapter (source-scoped references or a narrow helper channel), states that Worklease bearer credentials are never sent, and states the minimized inherited environment
- [x] It states the trust model: process isolation is not a sandbox, and an installed adapter runs with the user's privileges
- [x] It includes a compatibility and deprecation policy, and a JSON Schema for every message is committed for use by TASK-133.2 and TASK-133.3

## Definition of done

- [x] `mise run doc-test` passes and every changed relative link resolves
- [x] Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit

## Plan

1. Derive a versioned stdio method/manifest contract from built-in Backlog.md and GitHub adapters and the provider contract. 2. Specify bounded transport, identity, authentication, errors, pagination and receipts; commit JSON Schemas for each wire message. 3. Validate schemas/links and repository gates, review once, commit and merge with unrelated main changes preserved.

## Notes

Specified v1 JSON-RPC stdio protocol and shared message schemas; review found and corrections addressed lost-receipt recovery, strict mutation fields, found-item payloads, diagnostic/code matching, and optional capability groups. Verified doc-test, schema compilation, changed links, lint, format-check, test, typecheck; no source code changed.

Committed protocol and schema at cb496686ee925f42fc5f13d1f2b8fa6d5a76eb29. Merge to main deferred while unrelated staged edits in internal/cli/queue_write_claim*.go prevent Git merge; preserving those edits. Awaiting staged index to clear, then integrate and finalize.

Integrated cb496686ee925f42fc5f13d1f2b8fa6d5a76eb29 into main at 6941b36; doc-test passed again after merge. Single review pass resolved five concrete schema/recovery issues. Next step: TASK-133.2 may implement supervision against the committed contract.

Post-completion review (6800074): schema now requires host-checked readReceipt verified evidence, string semantics / integer limits for capabilities; doc specifies configSchema subset, cursor scope binding, and host retryAt gating. No follow-up.

## Summary

Specified versioned JSON-RPC stdio source adapter protocol, manifest, safety/credential/receipt rules, and schemas; updated proposal §11. Verified doc-test, schema compilation, links, lint, format-check, tests, typecheck, and pre-commit hooks. Committed cb49668 and merged as 6941b36.
