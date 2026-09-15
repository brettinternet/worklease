---
id: TASK-109.4
title: Ship a two-machine quickstart and clean-state acceptance journey
status: To Do
assignee: []
created_date: '2026-09-15 21:20'
updated_date: '2026-09-15 21:32'
labels:
  - remote-authority
  - ergonomics
dependencies:
  - TASK-109.1
  - TASK-109.2
  - TASK-109.3
parent_task_id: TASK-109
priority: high
type: docs
ordinal: 151000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The onboarding experience is only complete when a new user can follow the shortest supported path and the project continuously proves that path from empty state. The README remote section, `docs/remote-claim-authority.md`, `docs/container.md`, and `docs/remote-demo.tape` currently show `profile add --authority-id`, hand-written YAML, and `--json`/`sed` extraction of the authority ID. Replace that protocol-oriented guidance with a task-oriented server-to-client journey built on TASK-109.1 through TASK-109.3, regenerate the demo recording from the new commands, and guard the journey against regressions using the existing two-host acceptance harness from TASK-107.11.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The primary quickstart goes from fresh server installation to a remotely visible claim using only supported Worklease commands plus secure transfer of one enrollment artifact.
- [ ] #2 The primary quickstart contains no raw HTTP request, protocol header, `--json` parsing, manual authority-ID handling, or hand-edited YAML.
- [ ] #3 The quickstart clearly separates the default secure setup with a generated pinned certificate from an explicitly labeled temporary trusted-LAN cleartext test path.
- [ ] #4 The README remote section, `docs/remote-claim-authority.md`, `docs/container.md`, and the regenerated `docs/remote-demo.tape` and GIF use the new journey.
- [ ] #5 A clean-state two-host acceptance test executes the documented command journey, including guided init, serve, artifact transfer, enrollment, claim visibility, contention, heartbeat, and release.
- [ ] #6 Every documented command is exercised by automated documentation or acceptance validation.
- [ ] #7 Advanced profile management, bare-secret invites, and raw metadata diagnostics remain documented outside the primary happy path.
<!-- AC:END -->
