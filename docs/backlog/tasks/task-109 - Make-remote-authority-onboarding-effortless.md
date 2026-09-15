---
id: TASK-109
title: Make remote authority onboarding effortless
status: To Do
assignee: []
created_date: '2026-09-15 21:19'
updated_date: '2026-09-15 21:31'
labels:
  - remote-authority
  - ergonomics
dependencies: []
priority: high
type: feature
ordinal: 147000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A first-time administrator currently has to initialize the server, edit YAML, obtain TLS material from somewhere, restart, query a strict HTTP metadata endpoint or parse `--json` output for the authority ID, create a profile, move a bare-secret invite file, and enroll each machine while repeating `--profile` on every later command. This is secure but exposes protocol details and creates many opportunities for confusion. The largest single obstacle is transport: the default configuration is localhost-only cleartext, and a "secure" LAN deployment today requires a CA-signed certificate because the client has no way to pin a server certificate, so users are pushed toward insecure HTTP.

Target journey. Server host: `worklease server init` (guided) then `worklease serve`. Client host: receive one invite artifact, run `worklease enroll`, then `worklease acquire`. A self-signed certificate generated at init and pinned through the invite keeps the default secure without a PKI. Local coordination, existing explicit profile commands, and the raw metadata endpoint stay available outside the happy path.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A fresh administrator can configure and start a standard remote authority without manually editing configuration files or obtaining certificates from an external CA.
- [ ] #2 A fresh client can establish trust, create its profile, and enroll with one command and one securely transferred artifact.
- [ ] #3 The happy path never requires curl, raw protocol headers, `--json` parsing, manual authority-ID discovery, or a separate profile-add step.
- [ ] #4 Secure transport and authority pinning remain fail-closed: self-signed server certificates are pinned through the invite rather than trusted on first use, and insecure LAN testing requires an explicit, clearly warned opt-in.
- [ ] #5 After enrollment the recipient runs lifecycle commands without repeating `--profile` on every command.
- [ ] #6 The documented two-machine happy path is covered by executable acceptance testing from clean state.
<!-- AC:END -->
