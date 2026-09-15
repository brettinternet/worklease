---
id: TASK-109.4
title: Ship a two-machine quickstart and clean-state acceptance journey
status: To Do
assignee: []
created_date: '2026-09-15 21:20'
updated_date: '2026-09-15 21:56'
labels:
  - remote-authority
  - ergonomics
dependencies:
  - TASK-109.1
  - TASK-109.2
  - TASK-109.3
references:
  - cmd/worklease-remote-smoke/main.go
  - cmd/worklease-doc-test/main.go
  - scripts/test-remote-vm.sh
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
- [ ] #1 The primary quickstart uses Worklease commands and secure artifact transfer to initialize/start a server, enroll the first administrator from its bootstrap artifact, issue a write invite, enroll a separate client, and observe that client claim from the administrator; runtime secrets remain outside the checkout.
- [ ] #2 The primary quickstart contains no raw HTTP request, protocol header, `--json` parsing, manual authority-ID handling, or hand-edited YAML.
- [ ] #3 The quickstart clearly separates the default secure setup with a generated pinned certificate from an explicitly labeled temporary trusted-LAN cleartext test path.
- [ ] #4 The README remote section, `docs/remote-claim-authority.md`, `docs/container.md`, and the regenerated `docs/remote-demo.tape` and GIF use the new journey.
- [ ] #5 Extend the existing remote-smoke harness with isolated server/admin/client homes, generated pinned TLS, flag-driven guided init, serve readiness, secure artifact handoff, default-profile enrollment, doctor, cross-client claim visibility/contention, heartbeat, and release. A focused terminal test separately covers interactive setup/hidden input. Run the existing two-host VM harness to prove transport across hosts.
- [ ] #6 Every command in the new primary and explicitly insecure onboarding journeys is exercised by doc-test or acceptance validation. Regenerate the demo only with disposable credentials and inspect tape/GIF for secret disclosure; unrelated advanced-document command coverage is not expanded by this task.
- [ ] #7 Advanced profile management, bare-secret invites, and raw metadata diagnostics remain documented outside the primary happy path.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Once TASK-109.3 is complete, update README.md, docs/remote-claim-authority.md, docs/container.md and demo source around the exact secure command journey; keep legacy/recovery diagnostics in advanced sections.
2. Extend cmd/worklease-remote-smoke and cmd/worklease-doc-test, reusing scripts/test-remote-vm.sh rather than adding a parallel harness. Validate clean-state admin and write-client roles with separate homes and default selections.
3. Run doc-test, remote-smoke, remote-smoke-vm and repository quality gates; regenerate and inspect the disposable-credential demo. Record executed commands and cross-host evidence before checking acceptance.
<!-- SECTION:PLAN:END -->
