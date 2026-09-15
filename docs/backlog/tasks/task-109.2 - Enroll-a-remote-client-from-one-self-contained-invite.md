---
id: TASK-109.2
title: Enroll a remote client from one self-contained invite
status: To Do
assignee: []
created_date: '2026-09-15 21:20'
updated_date: '2026-09-15 21:56'
labels:
  - remote-authority
  - ergonomics
dependencies:
  - TASK-109.1
references:
  - internal/cli/profile_commands.go
  - internal/cli/remote_admin_commands.go
  - internal/config/profile.go
  - internal/authority/http.go
  - internal/authority/enrollment_test.go
parent_task_id: TASK-109
priority: high
type: feature
ordinal: 149000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Joining currently requires an explicit profile and a bare invite secret; profile default and hidden invite input already exist. Make one securely transferred artifact carry a versioned endpoint, authority ID, optional SHA-256 DER leaf-certificate pin, and invite secret. Reuse existing enrollment replay and owner-private profile storage rather than adding a second enrollment path.

The artifact itself is the out-of-band trust root: the transfer channel must provide confidentiality and authenticity. Encoding or a self-contained signature cannot authenticate a wholly substituted artifact. Reject malformed artifacts and mismatches against established trust; do not promise detection of complete replacement on a fresh client. Pin checks apply to every subsequent request, not only enrollment.

TASK-109.1 owns server configuration and certificate creation. This task owns the shared artifact codec, server-init and invite-issue producers, enrollment input, client/profile pin persistence, and successful default activation. Legacy bare-secret inputs and offline restore/bootstrap-reissue remain usable without silently changing their recovery semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 server init and invite issue emit the same bounded, versioned artifact containing endpoint, authority ID, optional SHA-256 DER leaf-certificate pin, and single-use invite secret. File output is owner-private and refuses unsafe paths/overwrites. Bootstrap retains admin role; issued invites default to write.
- [ ] #2 Fresh enrollment accepts one artifact without pre-creating a profile; --profile names the destination, with a documented deterministic name when omitted. Reuse requires matching endpoint, authority and pin, and must not overwrite an existing enrolled credential. Activate the profile and set a missing user default only after successful enrollment; preserve existing defaults and bindings.
- [ ] #3 For pinned HTTPS, verify the exact DER leaf-certificate SHA-256, endpoint hostname/IP and validity before sending any bearer; use normal CA verification when no pin exists. Persist the pin through activation and exact replay and enforce it on all CLI/MCP requests. Never follow redirects or silently accept a changed certificate.
- [ ] #4 The recipient does not need to run curl, copy an authority ID, run profile add, or pass `--profile` on subsequent commands.
- [ ] #5 Malformed/unsupported artifacts, conflicting established trust, wrong authority, certificate mismatch, expired invites, and redemption by a different installation fail closed. Exact durable enrollment retries remain idempotent rather than being mistaken for invite reuse. Insecure HTTP requires explicit client opt-in; the artifact alone cannot enable it. Document that whole-artifact substitution is prevented by authentic transfer, not by its encoding.
- [ ] #6 Invite and installation credentials never appear in argv, normal output, logs, or repository files.
- [ ] #7 Keep explicit profile add and bare-secret --invite-file, --invite-fd, and hidden-prompt enrollment working with a selected profile. Existing restore/bootstrap-reissue outputs remain redeemable; document how recovery users obtain trust/profile information without weakening restore-incarnation checks.
- [ ] #8 Tests cover file/fd/hidden-prompt artifact enrollment, malformed/oversized input, profile-name collisions, existing default/binding precedence, local-only fallback, pinned TLS on later CLI/MCP calls, wrong authority, expiry, distinct-installation replay rejection, exact retry after response loss, and profile-save failure without losing replay credentials.
- [ ] #9 invite issue defaults role to write and label to the selected issuer profile name, preserving explicit overrides; choosing admin remains explicit and issuing any invite still requires an administrative installation.
- [ ] #10 The artifact is a compact single-line token accepted from an owner-private file, inherited descriptor, or hidden terminal prompt. Non-terminal input without an explicit source fails immediately; conflicting sources fail. Tokens never appear in positional arguments, normal output, or echoed prompts.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Define a strict bounded versioned artifact codec and malformed-input tests; connect init and invite issue to it using TASK-109.1 configuration.
2. Extend config.Profile and the existing HTTP transport with persistent fail-closed pin validation; preserve it when activateEnrollment rebuilds the profile.
3. Extend current enrollment input handling and durable retry/profile activation, preserving profile selection precedence (flag, environment, binding, default, local).
4. Test secure happy paths and failure/retry boundaries, including bootstrap admin versus ordinary write invitations and legacy recovery inputs.
<!-- SECTION:PLAN:END -->
