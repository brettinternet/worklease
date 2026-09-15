---
id: TASK-109.1
title: Add guided remote server setup
status: Done
assignee:
  - '@brett'
created_date: '2026-09-15 21:19'
updated_date: '2026-09-15 23:15'
labels:
  - remote-authority
  - ergonomics
dependencies: []
references:
  - internal/cli/hosted_commands.go
  - internal/cli/hosted_commands_test.go
  - internal/server/server.go
modified_files:
  - CHANGELOG.md
  - docs/remote-claim-authority.md
  - internal/cli/commands.go
  - internal/cli/guided_setup.go
  - internal/cli/hosted_commands.go
  - internal/cli/hosted_commands_test.go
  - internal/cli/root_test.go
  - internal/server/server.go
  - internal/server/server_test.go
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
- [x] #1 Bare server init retains non-interactive local-only behavior. An explicit guided-setup option accepts flags or prompts for missing listen address, client-facing endpoint, transport, and admitted prefixes; fully specified flag-driven setup never prompts.
- [x] #2 Guided LAN setup defaults to TLS and generates owner-private certificate/key files without YAML editing or an external CA. The leaf certificate covers the advertised endpoint host/IP, has a documented validity period, and its paths and client-facing endpoint are persisted in validated server configuration.
- [x] #3 An existing certificate/key pair skips generation. Setup rejects unreadable, unsafe, mismatched-pair, or expired material before initializing the authority; a supplied leaf whose SAN does not cover the advertised endpoint host is a warning, not a rejection, since CA-verified clients may reach it by another name. Both TLS sources expose the same leaf-certificate fingerprint handoff.
- [x] #4 Guided setup requires explicit confirmation or equivalent flags for a non-loopback listener and a separate credential-exposure acknowledgement for cleartext. Non-loopback cleartext is never default; legacy bare-init loopback behavior is preserved.
- [x] #5 In guided mode, non-terminal input with missing required choices fails immediately with the exact flags to supply. Cancellation and validation failures do not initialize an authority or overwrite existing config, certificate, key, or invite files; partial-write failures give a safe recovery action.
- [x] #6 Success output states created paths, authority ID, SHA-256 of the DER leaf certificate for either TLS source, start command, and the exact bootstrap enrollment command `worklease enroll --invite-file FILE` (no `--profile`; the artifact carries the profile name), without secret values. TASK-109.2 owns artifact encoding and redemption.
- [x] #7 After successfully binding, serve reports its actual listen address, transport, and advertised endpoint on stderr; startup diagnostics do not corrupt structured stdout or imply readiness after a bind/TLS failure.
- [x] #8 Validation errors identify the invalid choice and provide a directly usable correction.
- [x] #9 Automated tests cover localhost, secure LAN with generated certificate, secure LAN with supplied certificate (including SAN-mismatch warning), explicitly insecure LAN, cancellation, non-interactive missing input, and invalid-input paths.
- [x] #10 Fresh guided setup defaults to task: and coordination: prefixes and preserves explicit overrides; legacy bare init and existing configurations retain their admitted-prefix behavior.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend guided server init inputs and validation while preserving the bare-init path.
2. Generate or validate TLS material, persist the advertised endpoint, and make setup writes atomic/no-overwrite.
3. Report setup handoff details and serve bind diagnostics.
4. Add focused coverage for guided, TLS, cancellation, validation, and compatibility paths.
5. Run focused and repository quality gates, review the diff, and finalize TASK-109.1.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented opt-in guided server initialization with interactive/non-interactive safety gates, generated or supplied TLS validation, advertised-endpoint persistence, handoff output, and post-bind serve diagnostics. Added focused coverage for legacy localhost behavior, generated/supplied TLS, SAN warnings, unsafe/mismatched/expired certificates, interactive defaults/cancellation, non-terminal required flags, insecure LAN acknowledgements, and prefix overrides. Focused tests and mise lint/format-check/test/typecheck pass.

Independent review found and implementation fixed output-path collision, EOF cancellation, listener/endpoint port validation, full supplied-chain expiry validation, crash recovery, shell quoting, and LAN TLS coverage gaps. A config-directory flock now serializes setup/recovery, and the bounded recovery journal is target-scoped and preserves custom invite recovery commands. Independent verifier passed all 10 acceptance criteria. Final evidence: go test -race ./internal/cli ./internal/server; mise run lint; mise run format-check; mise run test; mise run typecheck; git diff --check.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added opt-in guided remote server setup with safe interactive and flag-driven choices, generated or supplied TLS, persisted advertised endpoints, bounded crash recovery, actionable handoff output, and post-bind serve diagnostics. Preserved bare local-only init behavior and documented the 365-day generated certificate contract. Verified all acceptance criteria with focused/race tests, independent review and verification, and the repository lint, format, test, and typecheck gates.
<!-- SECTION:FINAL_SUMMARY:END -->
