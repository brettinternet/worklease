---
id: TASK-109.2
title: Enroll a remote client from one self-contained invite
status: To Do
assignee: []
created_date: '2026-09-15 21:20'
updated_date: '2026-09-15 21:32'
labels:
  - remote-authority
  - ergonomics
dependencies:
  - TASK-109.1
parent_task_id: TASK-109
priority: high
type: feature
ordinal: 149000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Joining a client currently requires out-of-band authority-ID discovery (`--json` parsing or the raw metadata endpoint), `profile add` with the endpoint and ID, moving a bare 64-hex invite secret, `enroll --profile`, and then `--profile` or `WORKLEASE_PROFILE` on every later command. The bootstrap invite written by `server init` has the same bare shape, so the first administrator enrollment has the same friction as every later one.

Make the invitation the single trust and enrollment handoff: the artifact carries the endpoint, authority ID, server certificate fingerprint when the server uses a self-signed certificate (TASK-109.1), and the invite secret, so the recipient only needs the file and one command. The client HTTP transport must verify a pinned fingerprint instead of relying on system CAs when a pin is present. `server init` and `invite issue` must emit the same artifact format so bootstrap and later invites are enrolled identically.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 An administrator can issue an owner-private, single-use enrollment artifact containing the endpoint, authority ID, optional server certificate SHA-256 fingerprint, and invite secret; `server init` writes its bootstrap invite in the same format.
- [ ] #2 From a fresh client state, one enroll command consumes the artifact, verifies the pinned authority and certificate, creates or selects the named profile, stores the installation credential, and sets that profile as the user default when no default exists.
- [ ] #3 When the artifact carries a certificate fingerprint, the client trusts only a server presenting that certificate, regardless of system CA trust; a mismatch fails closed before any credential is sent.
- [ ] #4 The recipient does not need to run curl, copy an authority ID, run profile add, or pass `--profile` on subsequent commands.
- [ ] #5 Tampering, endpoint redirection, wrong-authority responses, certificate mismatch, expiry, and invite replay fail closed with actionable errors.
- [ ] #6 Invite and installation credentials never appear in argv, normal output, logs, or repository files.
- [ ] #7 Existing explicit `profile add` and bare-secret `--invite-file` enrollment remain supported or receive a documented migration path.
- [ ] #8 Automated tests cover successful enrollment, default-profile selection, safe retry after uncertain outcomes, tampering, wrong authority, certificate mismatch, expiry, and replay.
<!-- AC:END -->
