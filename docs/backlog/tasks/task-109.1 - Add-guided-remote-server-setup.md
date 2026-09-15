---
id: TASK-109.1
title: Add guided remote server setup
status: To Do
assignee: []
created_date: '2026-09-15 21:19'
updated_date: '2026-09-15 21:49'
labels:
  - remote-authority
  - ergonomics
dependencies: []
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
- [ ] #1 The setup flow accepts by flag or interactively gathers the listen address, the client-facing endpoint URL when the listen address is not loopback, the transport choice, and admitted resource prefixes for common localhost and LAN deployments.
- [ ] #2 A standard LAN authority can be initialized without opening or editing YAML and without supplying externally issued TLS material: setup generates an owner-private self-signed certificate and key and records their paths in the configuration.
- [ ] #3 Supplying an existing certificate and key remains supported and skips generation.
- [ ] #4 Broad or cleartext listeners require explicit confirmation or an equivalent non-interactive flag and present a concise credential-exposure warning; cleartext on a non-loopback listener is never chosen by default.
- [ ] #5 When stdin is not a terminal and a required choice is missing, setup fails immediately with the exact flags to supply instead of prompting or silently defaulting to localhost.
- [ ] #6 Success output states what was created, the authority ID, the certificate SHA-256 fingerprint when generated, how to start the server, and the single next command for enrolling another machine, without printing secret material.
- [ ] #7 `serve` prints the listen address, transport, and client-facing endpoint on startup.
- [ ] #8 Validation errors identify the invalid choice and provide a directly usable correction.
- [ ] #9 Automated tests cover localhost, secure LAN with generated certificate, secure LAN with supplied certificate, explicitly insecure LAN, cancellation, non-interactive missing input, and invalid-input paths.
- [ ] #10 The default admitted prefixes cover the resources used in the README quickstart (for example `task:` and `coordination:`) so a first remote acquire is not rejected for admission without a deliberate choice.
<!-- AC:END -->
