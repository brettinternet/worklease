---
id: TASK-116
title: Preserve recovery paths in text error redaction
status: To Do
assignee: []
created_date: '2026-09-16 17:51'
updated_date: '2026-09-16 18:01'
labels: []
dependencies: []
references:
  - internal/output/output.go
  - internal/output/output_test.go
priority: medium
type: bug
ordinal: 158000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Text error rendering applies value-only token redaction to safe detail fields, so a contextual handle path containing its expected 64-hex context digest is printed as ctx-[REDACTED].json. The structured redactor already preserves SHA-256 values under path keys, but writeTextError discards that key context. This breaks the recovery pointer promised by the output contract while providing no additional credential protection.

## Implementation context
- Classify in internal/output/output.go already applies redactMap(details, true), preserving key context and removing public-output private payloads. writeTextError then calls Redact(failure.Details[key]) on each allowed value, losing the key and redacting the contextual filename digest a second time.
- writeText is an existing key-aware rendering example: redact(fields[key], key, public). Reuse the current policy instead of adding a second path-specific regex or a new redaction API.
- TestRedactKeepsHashAndPathFieldsButRedactsBareTokens in internal/output/output_test.go covers structured projection but never renders the failing text error. Build a classified error with pendingPath=/home/handles/ctx-<64 lowercase hex>.json and render it via writeTextError and WriteError.

## Scope and security boundaries
- Preserve fields already permitted by safeTextDetail; pendingPath is the path-valued recovery field currently in that allowlist. Do not expand text visibility to every *Path field, private payload, or arbitrary detail key just to achieve parity with JSON.
- Keep isPublicHexKey policy unchanged: structured hashes/path values are public under the current contract, while unclassified bare 64-hex strings and secret-key values remain redacted. This ticket repairs lost key context; it is not a redesign of what counts as a safe path.
- Preserve escapeText behavior, deterministic field ordering, and existing colored/uncolored behavior. Do not interpolate the path into the unstructured message or recoveryHint as a workaround: those strings still require value redaction.

## Coordination
No dependencies. This is a small output-only fix that can land before TASK-114/115 and will make their pendingPath recovery pointers usable. No lifecycle or error-classification behavior belongs here.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Text error output retains the exact contextual pendingPath including its 64-hex digest, subject only to existing control-character escaping. Only detail fields already allowed by safeTextDetail are shown; arbitrary path fields are not newly exposed.
- [ ] #2 Text and JSON error rendering preserve the same key-aware public redaction semantics for shared safe fields, without changing isPublicHexKey or weakening public private-payload filtering.
- [ ] #3 Tokens under secret keys, unclassified bare 64-hex values in messages/neutral fields, and nested private payloads remain redacted or omitted. A public contextual path digest is not treated as a credential merely because it is hex.
- [ ] #4 Renderer-level regression tests compare text and JSON for contextual pendingPath preservation, secret/neutral-value redaction, nested public filtering, and unchanged control-character escaping and detail allowlisting. Exercise colored and uncolored text without leaking credentials.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Add regression tests that fail before the fix and pass afterward; record the commands and evidence in this task without claiming unexecuted scenarios.
- [ ] #2 Run mise run lint, mise run format-check, mise run test, and mise run typecheck; stage intended changes and run mise run hooks before committing.
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend internal/output/output_test.go with a failing rendered pendingPath case using the existing structured redaction fixture pattern; assert both complete path and absence of a separate test token.
2. Remove the key-context loss in writeTextError using the existing key-aware public projection/rendering policy; retain allowlisting, escaping, ordering, and styling.
3. Run go test ./internal/output through mise and all repository gates. No documentation or lifecycle refactor is required.
<!-- SECTION:PLAN:END -->
