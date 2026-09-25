---
id: TASK-143.2
title: Deliver host-resolved credentials to external adapters over a narrow channel
status: Done
assignee: []
created_date: '2026-09-25 16:38'
updated_date: '2026-09-25 19:47'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies:
  - TASK-142.2
documentation:
  - docs/external-adapter-protocol.md
  - docs/external-adapter-protocol.schema.json
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-143
priority: medium
type: feature
ordinal: 74000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
External adapters receive only an opaque `credentialRef`, and the host strips provider tokens from their environment, so each adapter must implement its own keychain or secret lookup. That duplicates security-sensitive code in every adapter and bypasses the host's principal verification.

The Linear work in TASK-134 adds a user-configured credential helper for built-in adapters, and DRAFT-14 plans OAuth. This task makes the same host-side credential resolution available to external adapters, so helpers and later OAuth serve built-in and external sources alike. It depends on the Linear credential-helper subtask; add that dependency once the subtask exists.

The user wants the CLI approached with API-driven design: every command added here is a stable, scriptable interface first, with `--json` output in the existing CLI envelope (`schemaVersion`, `ok`, `operation`, and `error.reason`/`exitCode`/`details`), stable diagnostic reasons, and documented exit codes, and the human output is a rendering of that same result.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 An external source can name a host-side credential helper in queue.yaml; the host runs it and delivers the resulting secret to the adapter over one documented protocol channel, never on argv, in the environment, in source config, or in any logged or journaled message.
- [x] #2 The channel is a protocol v1-compatible optional feature negotiated through the manifest; adapters that do not declare it keep working unchanged with the opaque `credentialRef`.
- [x] #3 The credential is bound to the approved source, origin, and principal; changing the helper, its scope, or the principal requires reapproval, and a mismatch is refused with a stable diagnostic.
- [x] #4 Expiry and refresh are supported: the host can deliver a replacement credential without restarting the adapter, and refresh is serialized per credential.
- [x] #5 The protocol spec and JSON schema define the channel and its failure diagnostics, and the conformance check verifies that the credential never appears in adapter stdout, stderr, diagnostics, or host logs.
- [x] #6 The authoring guide shows an adapter consuming the credential.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Permit a scoped external credentialHelper and bind command, source configuration (including HTTPS origin), principal, and credential reference to explicit adapter approval. 2. Negotiate an optional v1 credential-delivery feature; reuse the bounded host helper to verify principal through the approved adapter and deliver only by a dedicated JSON-RPC method on stdin before resolve and before subsequent operations (refresh), with redaction and stable failures. 3. Add focused config/approval/process/conformance regression tests, update protocol/schema/author guide, then run project gates and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented optional host-credential-v1 stdin delivery for external sources. Approval now binds helper command, config/origin and account while retaining legacy approval digests. Host verifies adapter-reported principal and origin, serializes refresh through dispatch, refuses drift/expiry, and never logs tokens. Added protocol/schema/queue/author guide and focused config, approval, concurrency, redaction and conformance tests. Reviewer found four concrete risks (interleaved write, read principal drift, 64KiB stderr leak, wrong-origin diagnostic); corrected all and added targeted tests. Focused race tests passed count=3; lint, format-check, typecheck, e2e, and full suite with isolated Go cache passed before final test-only assertion; final staged hook test still in progress.

Acceptance evidence: #1 TestQueueExternalAdapterConfiguration parses account/helper/HTTPS origin; TestExternalAdapterHostCredentialRefreshAndScope runs a real helper and supervised adapter, observes two credential deliveries on one process; host request is stdin only, redacted process diagnostics and report contain no token. #2 TestExternalAdapterRejectsUndeclaredCredentialFeature refuses unnegotiated helper; existing opaque credentialRef tests and legacy approval digest test pass. #3 approval mutation checks reject changed helper, origin and account; mismatch tests cover wrong principal/origin and post-resolve read principal drift. #4 expiry test refuses expired credential, refresh test replaces token without restart, concurrent rotation write test proves no unverified Bob-side dispatch. #5 TestExternalAdapterCredentialConformanceLeak exercises clean, stdout echo, stderr echo and >64KiB padded stderr leak and verifies the report has no sentinel; schema and spec define credential method and scope diagnostic. #6 authoring guide includes helper YAML and credential request/response walkthrough. Focused race count=3, full mise test, lint, format-check, typecheck, hooks, e2e/doc-test pass (with isolated Go build cache and reduced parallelism for competing worktrees). Reviewed four defects and fixed them. Commit 296ab62; main merge e6c3eb7; Worktrunk removal deleted task-143-2-credentials branch and checkout; Herdr workspace wR2 closed.

Final delivery: merge conflict in docs/queue.md resolved against the shipped Linear documentation, full suite and hooks passed on merged tree, and main merge/worktree teardown completed. No remaining blocker; next TASK-143 subtask is TASK-143.3 by ordinal (or TASK-143.4 independently).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Delivered approved, scope-verified, refreshable host credentials over the optional external adapter v1 channel. Conformance tests cover secret leakage and write/read races; race tests, full suite, lint, format, typecheck, hooks and e2e passed. Merged to main at e6c3eb7 and removed the owned worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
