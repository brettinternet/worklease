---
id: TASK-90
title: Decide and enforce one redaction policy for typed output
status: Done
assignee:
  - '@pi-01a097fa'
created_date: '2026-09-12 22:39'
updated_date: '2026-09-13 00:00'
labels:
  - go-rewrite
dependencies: []
references:
  - internal/output/output.go
priority: medium
type: enhancement
ordinal: 115000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
output.Redact only traverses map[string]any, []any, and strings. Typed structs such as lease.Receipt, lease.ClaimView, and ledger.Operation pass through untouched, so the argv, checkpoint, evidence, and output entries in isSecretKey only affect loosely typed detail maps and are inconsistent across CLI JSON, MCP structuredContent, and op inspect --full. The contract (section 6.3) says the invoking exec, checkpoint, and replace command may return their own payload and that credential-authenticated op inspect --full may return the receipt, checkpoint, and evidence, while public views never expose them. Nothing leaks today because no struct carries a token, but the guarantee is by construction rather than enforced, and the secret-key list contradicts the contract for the authenticated views.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A written decision records which keys are always redacted (bearer material), which are allowed only on the invoking command and authenticated --full inspection, and which are public
- [x] #2 Redaction reaches every emitted value, including typed structs, or the code documents why struct fields are exempt and a test enumerates the exempt types
- [x] #3 op inspect --full, exec, checkpoint, and replace-file receipts return argv, checkpoint, and evidence exactly as the contract permits, and public status, list, history, events, and MCP projections never do
- [x] #4 Tests assert both the allowed and forbidden cases on CLI JSON, text, and MCP structuredContent
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Define two explicit output policies: always redact bearer/credential material; additionally omit private payload keys on public projections. Normalize typed values through exact JSON before recursive redaction.
2. Apply public redaction to public CLI and MCP boundaries while preserving checkpoint, exec, replace-file, and credential-authenticated op inspect --full payloads.
3. Add tests across typed output, CLI JSON/text, and MCP structuredContent, then document the policy and run focused and repository gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Follow-up review TASK-94 reproduced a concrete MCP leak: bearer-shaped resource/agent/work metadata bypasses output.Redact through typed slices and claim projections. TASK-94 fixes the MCP serialization boundary with exact-number JSON normalization; this item retains the broader CLI authenticated/public output policy and suffix-based hash/path exemptions.

Implemented typed JSON normalization, explicit private/public redaction policies, public CLI/MCP boundary selection, and focused JSON/text/MCP coverage in the isolated TASK-90 worktree. Focused packages pass.

Independent security review found two defects: bearer-shaped JSON member names were not redacted and watch event revisions could lose precision. Both were fixed with deterministic key redaction/collision handling and direct typed watch projection.
Validation after integrating current main: mise run lint; mise run format-check; mise run test; mise run typecheck; mise run hooks. Focused evidence: TestTypedPrivateAndPublicOutputPolicies, TestJSONAndTextWritersApplySelectedPolicy, TestInvokingExecReturnsArgvAndOutputInJSONAndText, TestLedgerCLIJSONAndPendingHandleReconciliationRecovery, TestPublicFullHistoryAndEventsRedactCheckpointAndCredentials, TestWatchJSONRedactsEventDetails, and MCP typed projection tests.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Defined and enforced typed output redaction: bearer material is always removed, public CLI/MCP views additionally redact private operation payloads, and invoking/authenticated views retain permitted payloads. Added JSON/text/MCP coverage, deterministic map-key redaction, and exact watch integer preservation. Implemented in f104723 and integrated on main at f9de891; all repository gates and hooks pass.
<!-- SECTION:FINAL_SUMMARY:END -->
