---
id: TASK-121
title: Preserve holder details in remote contention errors
status: To Do
assignee: []
created_date: '2026-09-16 23:57'
updated_date: '2026-09-17 00:10'
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
- [ ] #1 Remote already-claimed responses preserve the bounded redacted holder projection through HTTP and CLI layers
- [ ] #2 Text output identifies the contended resource and the holders claim ID, agent ID, work key, and expiry, and does not present the failed request claim ID as the holder
- [ ] #3 Structured output distinguishes request or operation identifiers from holder metadata and exposes no credential, checkpoint, private evidence, or unapproved claim fields
- [ ] #4 Definitive no-commit contention removes the attempted pending handle while retaining actionable holder details and truthful commit state
- [ ] #5 Focused local and remote tests cover singleton and multi-resource contention, missing optional holder metadata, redaction, and text and JSON output
- [ ] #6 Malformed, oversized, or unexpected holder fields cannot leak private data or inject terminal output; valid allowlisted fields survive, and incomplete optional metadata still yields an actionable already-claimed error.
- [ ] #7 Commit state remains computed from the existing validated response/dispatch logic, never trusted from arbitrary wire details; unknown transport outcomes retain their pending records and the existing holder claim is never mutated by contention cleanup.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend HTTP error decoding for already-claimed with a strict bounded resource/holder projection matching the local service; ignore unknown/private fields and handle malformed optional metadata without hiding contention.
2. Reuse local text rendering and JSON redaction. Label attempted request/operation IDs distinctly from holder.claimId; do not forward arbitrary server messages or wire commitState.
3. Test a real remote contention end-to-end in text/JSON, singleton and multi-resource cases, and missing optional fields; add adversarial wire-detail tests for nested private fields, wrong types, excessive lengths, and control characters.
4. Assert definitive contention clears only the failed pending request/handle, leaves the holder untouched, and retains holder diagnostics; ambiguous transport outcomes still preserve exact pending recovery state.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Validation: HTTPClient.do in internal/authority/http.go reconstructs remote errors as remote request failed and copies only admittedPrefixes. The lease service already emits resource plus holder claimId/agentId/workKey/expiresAt, and internal/output/output.go already supports that bounded public shape. Preserve this existing projection rather than forwarding arbitrary server details. The HTTP mutation layer computes commitState and clears definitive requests; remote_lifecycle.go removes failed acquire handles only for definitive outcomes. Keep those safety decisions independent of wire detail contents.
<!-- SECTION:NOTES:END -->
