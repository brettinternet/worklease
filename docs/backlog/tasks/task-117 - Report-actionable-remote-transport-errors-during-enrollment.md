---
id: TASK-117
title: Report actionable remote transport errors during enrollment
status: Done
assignee:
  - '@brett'
created_date: '2026-09-16 23:48'
updated_date: '2026-09-17 04:02'
labels: []
dependencies: []
references:
  - internal/authority/http.go
  - internal/output/output.go
  - internal/cli/doctor_commands.go
  - internal/cli/remote_commands_test.go
  - internal/authority/enrollment_test.go
  - internal/authority/commit_truth_test.go
modified_files:
  - docs/remote-claim-authority.md
  - internal/authority/enrollment_test.go
  - internal/authority/http.go
  - internal/cli/doctor_commands.go
  - internal/cli/remote_commands_test.go
  - internal/reason/reason.go
priority: medium
type: bug
ordinal: 159000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Enrollment begins with unauthenticated metadata discovery. When the advertised endpoint cannot be reached—for example, an invite advertises https://127.0.0.1:8443 but enrollment runs on another machine—internal/authority/http.go returns the raw error from http.Client.Do. internal/output/output.go cannot map that error to the stable reason vocabulary, so the CLI reports `internal: unexpected internal failure` with `commitState: not-committed`. The no-commit state is truthful because enrollment mutation has not started, but the reason hides the actionable connectivity problem. Preserve the existing conservative commit-truth behavior for dispatched mutations while making pre-enrollment and other safe remote reachability failures understandable.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 An enrollment whose metadata endpoint refuses or cannot establish a connection reports a stable, actionable non-internal error instead of `internal: unexpected internal failure`.
- [x] #2 The enrollment failure remains `commitState: not-committed`, does not create an enrolled profile, and does not expose invite, installation, URL-embedded, or lower-level sensitive data.
- [x] #3 Transport classification preserves conservative mutation semantics: an error after a mutating request may have been dispatched remains an unknown outcome and retains its pending recovery record.
- [x] #4 Remote doctor continues to distinguish at least DNS failure, connection refusal, timeout, and TLS/pin failure with its existing actionable checks.
- [x] #5 Automated regression coverage reproduces the enrollment metadata transport failure through the client or CLI boundary and verifies the public text and JSON error envelopes.
- [x] #6 Deterministic coverage includes metadata DNS failure, refusal, timeout, and TLS/pin failure without real external services; public messages omit raw transport errors and URL credentials/query data.
- [x] #7 Any new public reason has a registered exit code and documentation; safe transport classification does not treat a received enrollment mutation error or dropped response as proof that the invite was unused.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add deterministic HTTP-client and CLI regression coverage for metadata DNS, refusal, timeout, and TLS/pin transport failures, including redaction and no-profile assertions.
2. Introduce a stable sanitized public transport classification at the safe pre-enrollment metadata boundary, reusing existing reason/exit-code infrastructure where possible.
3. Verify mutating enrollment dispatch still produces unknown commit truth and retains pending recovery state.
4. Run focused tests, repository quality gates, independent review, and merge the committed worktree change into main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Validation: HTTPClient.do in internal/authority/http.go returns http.Client.Do errors unchanged; HTTPClient.Enroll calls Metadata before its mutating enrollment request. internal/output/output.go maps untyped errors to internal. Remote doctor already classifies DNS/refusal/timeout/TLS at its own boundary. Keep that taxonomy intact and do not forward raw net/url error strings. This task is independent of holder projection (TASK-121), although both touch the HTTP client.

Implemented remote-transport-failure (exit 75) with sanitized DNS/refused/timeout/TLS/connect detail at the HTTP transport boundary. Caller cancellation/deadline remains interrupted, while client-side transport deadlines classify as timeout. Mutating requests still convert dispatch failures to unknown-outcome and retain pending recovery state. Verification passed: focused Go tests; mise run lint; mise run format-check; mise run test; mise run typecheck; staged mise run hooks; post-merge mise run test. Deterministic tests cover metadata DNS, refusal, transport timeout including client deadline, TLS/pin, text/JSON envelopes, redaction, no profile/credential creation, and doctor taxonomy. Existing TestRemoteMutationWritesBeforeDispatchAndRetainsUncertainty verifies dropped mutating responses retain pending recovery.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added stable, actionable, redacted remote transport failures for enrollment metadata discovery while preserving conservative unknown outcomes for dispatched mutations. Registered and documented the new exit-75 reason, preserved doctor DNS/refusal/timeout/TLS checks, and added deterministic envelope, redaction, persistence, and taxonomy coverage. Implementation commit ec19720 merged to main; all repository quality gates, hooks, and post-merge tests passed.
<!-- SECTION:FINAL_SUMMARY:END -->
