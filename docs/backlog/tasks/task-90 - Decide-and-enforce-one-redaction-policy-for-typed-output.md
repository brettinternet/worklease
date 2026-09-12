---
id: TASK-90
title: Decide and enforce one redaction policy for typed output
status: To Do
assignee:
  - '@brett'
created_date: '2026-09-12 22:39'
updated_date: '2026-09-12 22:42'
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
- [ ] #1 A written decision records which keys are always redacted (bearer material), which are allowed only on the invoking command and authenticated --full inspection, and which are public
- [ ] #2 Redaction reaches every emitted value, including typed structs, or the code documents why struct fields are exempt and a test enumerates the exempt types
- [ ] #3 op inspect --full, exec, checkpoint, and replace-file receipts return argv, checkpoint, and evidence exactly as the contract permits, and public status, list, history, events, and MCP projections never do
- [ ] #4 Tests assert both the allowed and forbidden cases on CLI JSON, text, and MCP structuredContent
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Follow-up review TASK-94 reproduced a concrete MCP leak: bearer-shaped resource/agent/work metadata bypasses output.Redact through typed slices and claim projections. TASK-94 fixes the MCP serialization boundary with exact-number JSON normalization; this item retains the broader CLI authenticated/public output policy and suffix-based hash/path exemptions.
<!-- SECTION:NOTES:END -->
