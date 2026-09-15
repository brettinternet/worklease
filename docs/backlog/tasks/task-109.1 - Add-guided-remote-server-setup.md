---
id: TASK-109.1
title: Add guided remote server setup
status: To Do
assignee: []
created_date: '2026-09-15 21:19'
updated_date: '2026-09-15 22:25'
labels:
  - remote-authority
  - ergonomics
dependencies: []
references:
  - internal/cli/hosted_commands.go
  - internal/cli/hosted_commands_test.go
  - internal/server/server.go
parent_task_id: TASK-109
priority: high
type: feature
ordinal: 148000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`server init` currently writes a fixed localhost-only, cleartext configuration and prints only a completion line; the authority ID is available only from `--json`, and `serve` prints nothing about where it listens. A LAN administrator must discover and edit the deployment YAML, provision TLS material from an external CA, and restart. Because the client cannot pin a certificate today, "secure LAN" effectively means "get a real certificate", which pushes users toward `allowInsecureHTTP`.

Provide a supported setup path that gathers the small set of deployment choices (listen address, client-facing endpoint, transport, admitted prefixes) interactively or by flags, generates a self-signed server certificate when no certificate is supplied, and leaves the administrator with a ready-to-run configuration. Setup must expose the certificate fingerprint and authority ID so the invite artifact (TASK-109.2) can carry them. Keep `server init` with no flags backward compatible for existing local-only users and scripts.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Bare server init retains non-interactive local-only behavior. An explicit guided-setup option accepts flags or prompts for missing listen address, client-facing endpoint, transport, and admitted prefixes; fully specified flag-driven setup never prompts.
- [ ] #2 Guided LAN setup defaults to TLS and generates owner-private certificate/key files without YAML editing or an external CA. The leaf certificate covers the advertised endpoint host/IP, has a documented validity period, and its paths and client-facing endpoint are persisted in validated server configuration.
- [ ] #3 An existing certificate/key pair skips generation. Setup rejects unreadable, unsafe, mismatched-pair, or expired material before initializing the authority; a supplied leaf whose SAN does not cover the advertised endpoint host is a warning, not a rejection, since CA-verified clients may reach it by another name. Both TLS sources expose the same leaf-certificate fingerprint handoff.
- [ ] #4 Guided setup requires explicit confirmation or equivalent flags for a non-loopback listener and a separate credential-exposure acknowledgement for cleartext. Non-loopback cleartext is never default; legacy bare-init loopback behavior is preserved.
- [ ] #5 In guided mode, non-terminal input with missing required choices fails immediately with the exact flags to supply. Cancellation and validation failures do not initialize an authority or overwrite existing config, certificate, key, or invite files; partial-write failures give a safe recovery action.
- [ ] #6 Success output states created paths, authority ID, SHA-256 of the DER leaf certificate for either TLS source, start command, and the exact bootstrap enrollment command `worklease enroll --invite-file FILE` (no `--profile`; the artifact carries the profile name), without secret values. TASK-109.2 owns artifact encoding and redemption.
- [ ] #7 After successfully binding, serve reports its actual listen address, transport, and advertised endpoint on stderr; startup diagnostics do not corrupt structured stdout or imply readiness after a bind/TLS failure.
- [ ] #8 Validation errors identify the invalid choice and provide a directly usable correction.
- [ ] #9 Automated tests cover localhost, secure LAN with generated certificate, secure LAN with supplied certificate (including SAN-mismatch warning), explicitly insecure LAN, cancellation, non-interactive missing input, and invalid-input paths.
- [ ] #10 Fresh guided setup defaults to task: and coordination: prefixes and preserves explicit overrides; legacy bare init and existing configurations retain their admitted-prefix behavior.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend internal/cli/hosted_commands.go and internal/server/server.go for opt-in setup and persisted advertised endpoint; preserve existing lock, no-overwrite, and secret staging protections.
2. Implement generated/supplied TLS validation and bind-success diagnostics. Freeze the persisted endpoint and SHA-256 DER leaf-certificate contract for TASK-109.2; do not implement a second invite codec here.
3. Add hosted CLI/server tests for terminal and non-terminal flows, TLS files, cancellation, partial failures, reruns, and legacy defaults.
<!-- SECTION:PLAN:END -->
