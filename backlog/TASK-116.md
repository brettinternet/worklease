---
id: TASK-116
title: Preserve recovery paths in text error redaction
status: done
type: bug
priority: medium
assignees: ['@brett']
rank: aeb57213c2eb57144
refs: [internal/output/output.go, internal/output/output_test.go]
files: [internal/output/output.go, internal/output/output_test.go]
created: 2026-09-16T17:51:00Z
updated: 2026-09-16T21:38:00Z
---

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

## Acceptance criteria

- [x] Text error output retains the exact contextual pendingPath including its 64-hex digest, subject only to existing control-character escaping. Only detail fields already allowed by safeTextDetail are shown; arbitrary path fields are not newly exposed.
- [x] Text and JSON error rendering preserve the same key-aware public redaction semantics for shared safe fields, without changing isPublicHexKey or weakening public private-payload filtering.
- [x] Tokens under secret keys, unclassified bare 64-hex values in messages/neutral fields, and nested private payloads remain redacted or omitted. A public contextual path digest is not treated as a credential merely because it is hex.
- [x] Renderer-level regression tests compare text and JSON for contextual pendingPath preservation, secret/neutral-value redaction, nested public filtering, and unchanged control-character escaping and detail allowlisting. Exercise colored and uncolored text without leaking credentials.

## Definition of done

- [x] Add regression tests that fail before the fix and pass afterward; record the commands and evidence in this task without claiming unexecuted scenarios.
- [x] Run mise run lint, mise run format-check, mise run test, and mise run typecheck; stage intended changes and run mise run hooks before committing.

## Plan

1. Add renderer-level regression coverage for contextual pendingPath preservation, text/JSON key-aware redaction parity, secret and neutral token handling, nested filtering, escaping, allowlisting, and color modes.
2. Update writeTextError to apply the existing key-aware public redaction policy while preserving safeTextDetail, ordering, escaping, and styling.
3. Run focused output tests and all repository quality gates, stage and run hooks, review the diff, commit, merge to main, finalize the task, and clean up the owned worktree.

## Notes

Implemented key-aware public redaction in writeTextError and added renderer-level regression coverage for text/JSON path preservation, safe-detail allowlisting, secret and neutral token redaction, nested private-payload filtering, escaping, and colored/uncolored text.
Evidence: before the fix, the focused internal/output test failed because pendingPath rendered as ctx-[REDACTED].json; after the fix it passed. mise run lint, mise run format-check, and mise run typecheck passed. mise run test initially exposed the developer default remote profile; rerunning with isolated HOME and the existing mise installation passed all packages. Staged mise run hooks passed, and the commit hook passed the full suite. Implementation commit: 1c1dd61.

Post-completion review (commit ee88ba8) re-verified this output-only fix and found no defect: text rendering preserves only the pendingPath recovery digest, while recoveryHint, holder, claimId, and resource values containing bare 64-hex strings remain redacted, and nested private-payload filtering and isPublicHexKey policy are unchanged.

## Summary

Preserved contextual pendingPath digests in text errors by retaining the detail key during public redaction, without expanding the text allowlist or weakening credential filtering. Added text/JSON regression coverage for recovery paths and security boundaries. Verified with focused tests, all required repository gates, staged hooks, and commit 1c1dd61.
