---
id: TASK-34
title: Improve actionable CLI errors
status: done
type: enhancement
priority: medium
assignees: ['@codex-loop-fresh-20260715-worklease-pass']
labels: [cli, ux]
rank: a3dfa9c4b73dfa98e
created: 2026-07-15T23:27:00Z
updated: 2026-07-15T23:36:00Z
---

Make human-readable CLI failures more actionable without changing opaque-resource semantics or the schema-versioned JSON error envelope. Missing status resource arguments should point to a working example; choice and required-argument failures should expose safe guidance; unknown policy names should show available policy values. A caller-supplied resource that has no claim remains a valid free status, because resources are opaque and there is no registry to query.

## Acceptance criteria

- [x] Text parser failures provide safe command-specific guidance for missing required values and invalid choices without echoing secrets or arbitrary rejected arguments.
- [x] Status missing or empty --resource suggests a copyable worklease status --resource example, while an unknown resource value still reports free rather than a false not-found error.
- [x] Human-readable unknown-policy errors list available policy names, JSON error envelopes and exit codes remain compatible, and regression tests cover the new guidance.

## Plan

1. Preserve the opaque-resource status contract and schema-versioned JSON output while tracing parser and text error rendering.
2. Add safe, command-specific text hints for missing required values and invalid choices, including the status resource example.
3. Render available policy values in human-readable unknown-policy errors without exposing rejected secrets or arbitrary arguments.
4. Add regression tests for parser guidance, policy suggestions, unknown-resource free status, then run quality gates and finalize.

## Notes

Implemented centralized safe parser hints for missing required options, missing option values, and invalid choices; status resource failures now show a copyable example. Text unknown-policy errors now render the existing AVAILABLE policy list. JSON parser errors remain unchanged, rejected values and secrets are never echoed, and unknown opaque resources still report free. README documents HINT and AVAILABLE fields. Focused tests, full quality gates, smoke scenarios, and staged hooks pass.

Independent verifier PASS: parser hints are safe, status missing/empty resource cases suggest the copyable example, arbitrary unknown resources remain free, unknown policy text and JSON expose AVAILABLE values, JSON envelopes and exit codes remain compatible, and existing help examples pass.
Verification: focused error/help tests, full mise run lint, format-check, test, typecheck, staged mise run hooks, and direct smoke scenarios all passed.

## Summary

Improved human-readable CLI failures with safe command-specific hints for missing required arguments and invalid choices, status resource examples for missing or empty values, and AVAILABLE policy suggestions. Preserved opaque-resource free status and JSON error envelopes/exit codes. Added regression coverage and documented the text grammar. Independent verifier PASS; full quality gates, staged hooks, and smoke checks passed.
