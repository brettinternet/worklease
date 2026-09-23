---
id: TASK-121
title: Preserve holder details in remote contention errors
status: Done
assignee:
  - '@brett'
created_date: '2026-09-16 23:57'
updated_date: '2026-09-17 02:47'
labels:
  - cli
  - remote
  - ux
dependencies: []
references:
  - TASK-78
  - internal/cli/remote_lifecycle.go
  - internal/cli/text.go
  - docs/cli-reference.md
  - internal/authority/http.go
  - internal/output/output.go
  - internal/output/output_test.go
  - internal/cli/remote_commands_test.go
  - internal/authority/regression_test.go
  - internal/authority/commit_truth_test.go
modified_files:
  - docs/cli-reference.md
  - internal/authority/authority_test.go
  - internal/authority/http.go
  - internal/cli/lease_commands.go
  - internal/cli/remote_commands_test.go
  - internal/cli/resource_commands_test.go
  - internal/lease/bundle_test.go
  - internal/output/output.go
  - internal/output/output_test.go
priority: high
type: bug
ordinal: 163000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A remote acquire that loses contention currently reports only remote request failed and decorates the error with the failed contenders generated claim ID. Operators can mistake that ID for the holder and cannot see which active claim owns the resource or when it expires, despite the authority having redacted holder metadata.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Remote already-claimed responses preserve the bounded redacted holder projection through HTTP and CLI layers
- [x] #2 Text output identifies the contended resource and the holders claim ID, agent ID, work key, and expiry, and does not present the failed request claim ID as the holder
- [x] #3 Structured output distinguishes request or operation identifiers from holder metadata and exposes no credential, checkpoint, private evidence, or unapproved claim fields
- [x] #4 Definitive no-commit contention removes the attempted pending handle while retaining actionable holder details and truthful commit state
- [x] #5 Focused local and remote tests cover singleton and multi-resource contention, missing optional holder metadata, redaction, and text and JSON output
- [x] #6 Malformed, oversized, or unexpected holder fields cannot leak private data or inject terminal output; valid allowlisted fields survive, and incomplete optional metadata still yields an actionable already-claimed error.
- [x] #7 Commit state remains computed from the existing validated response/dispatch logic, never trusted from arbitrary wire details; unknown transport outcomes retain their pending records and the existing holder claim is never mutated by contention cleanup.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend remote HTTP contention error decoding with a strict bounded holder/resource projection while keeping commit-state computation local.
2. Render holder and attempted request identifiers distinctly through existing text and structured output redaction.
3. Add focused local and remote regression coverage for singleton/multi-resource contention, optional and adversarial fields, cleanup, and ambiguous outcomes.
4. Run focused checks, full repository quality gates, independent review, then finalize and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Validation: HTTPClient.do in internal/authority/http.go reconstructs remote errors as remote request failed and copies only admittedPrefixes. The lease service already emits resource plus holder claimId/agentId/workKey/expiresAt, and internal/output/output.go already supports that bounded public shape. Preserve this existing projection rather than forwarding arbitrary server details. The HTTP mutation layer computes commitState and clears definitive requests; remote_lifecycle.go removes failed acquire handles only for definitive outcomes. Keep those safety decisions independent of wire detail contents.

Implemented bounded allowlisted remote holder decoding and output projection, with distinct requestClaimId labeling for contention and wait-timeout failures. Reviewer found two gaps (wait-timeout labeling and remote multi-resource coverage); both were fixed and retested.

Validation: mise run lint; mise run format-check; isolated HOME/XDG go test ./...; isolated HOME/XDG go vet ./...; focused authority/output/CLI/lease contention tests; staged mise run hooks. Commit 0f89dc8b44454be0080615d0872bd53d128500e5 merged to main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Preserved safe holder metadata across remote contention errors, distinguished attempted request IDs from holder claim IDs, and retained existing commit-state and pending-handle safety behavior. Added adversarial, singleton, multi-resource, wait-timeout, rollback, text, and JSON coverage; all repository gates passed with host configuration isolated.
<!-- SECTION:FINAL_SUMMARY:END -->
