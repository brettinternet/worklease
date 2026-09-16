---
id: TASK-117
title: Report actionable remote transport errors during enrollment
status: To Do
assignee: []
created_date: '2026-09-16 23:48'
labels: []
dependencies: []
references:
  - internal/authority/http.go
  - internal/output/output.go
  - internal/cli/doctor_commands.go
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
- [ ] #1 An enrollment whose metadata endpoint refuses or cannot establish a connection reports a stable, actionable non-internal error instead of `internal: unexpected internal failure`.
- [ ] #2 The enrollment failure remains `commitState: not-committed`, does not create an enrolled profile, and does not expose invite, installation, URL-embedded, or lower-level sensitive data.
- [ ] #3 Transport classification preserves conservative mutation semantics: an error after a mutating request may have been dispatched remains an unknown outcome and retains its pending recovery record.
- [ ] #4 Remote doctor continues to distinguish at least DNS failure, connection refusal, timeout, and TLS/pin failure with its existing actionable checks.
- [ ] #5 Automated regression coverage reproduces the enrollment metadata transport failure through the client or CLI boundary and verifies the public text and JSON error envelopes.
<!-- AC:END -->
