---
id: TASK-109.4
title: Ship a two-machine quickstart and clean-state acceptance journey
status: Done
assignee:
  - '@brett'
created_date: '2026-09-15 21:20'
updated_date: '2026-09-16 02:16'
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
modified_files:
  - README.md
  - cmd/worklease-doc-test/main.go
  - cmd/worklease-remote-smoke/main.go
  - docs/container.md
  - docs/remote-claim-authority.md
  - docs/remote-demo.gif
  - docs/remote-demo.tape
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
- [x] #1 The primary quickstart uses Worklease commands and secure artifact transfer to initialize/start a server, enroll the first administrator from its bootstrap artifact, issue a write invite, enroll a separate client, and observe that client claim from the administrator; runtime secrets remain outside the checkout.
- [x] #2 The primary quickstart contains no raw HTTP request, protocol header, `--json` parsing, manual authority-ID handling, or hand-edited YAML.
- [x] #3 The quickstart clearly separates the default secure setup with a generated pinned certificate from an explicitly labeled temporary trusted-LAN cleartext test path.
- [x] #4 The README remote section, `docs/remote-claim-authority.md`, `docs/container.md`, and the regenerated `docs/remote-demo.tape` and GIF use the new journey.
- [x] #5 Extend the existing remote-smoke harness with isolated server/admin/client homes, generated pinned TLS, flag-driven guided init, serve readiness, secure artifact handoff, default-profile enrollment, doctor, cross-client claim visibility/contention, heartbeat, and release. A focused terminal test separately covers interactive setup/hidden input. Run the existing two-host VM harness to prove transport across hosts.
- [x] #6 Every command in the new primary and explicitly insecure onboarding journeys is exercised by doc-test or acceptance validation. Regenerate the demo only with disposable credentials and inspect tape/GIF for secret disclosure; unrelated advanced-document command coverage is not expanded by this task.
- [x] #7 Advanced profile management, bare-secret invites, and raw metadata diagnostics remain documented outside the primary happy path.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Once TASK-109.3 is complete, update README.md, docs/remote-claim-authority.md, docs/container.md and demo source around the exact secure command journey; keep legacy/recovery diagnostics in advanced sections.
2. Extend cmd/worklease-remote-smoke and cmd/worklease-doc-test, reusing scripts/test-remote-vm.sh rather than adding a parallel harness. Validate clean-state admin and write-client roles with separate homes and default selections.
3. Run doc-test, remote-smoke, remote-smoke-vm and repository quality gates; regenerate and inspect the disposable-credential demo. Record executed commands and cross-host evidence before checking acceptance.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the guided onboarding journey across README.md, docs/remote-claim-authority.md, docs/container.md, and docs/remote-demo.tape. The remote acceptance harness now provisions both local and real-host authorities through flag-driven guided TLS setup, transfers self-contained artifacts, enrolls default profiles in isolated roots, runs doctor, and preserves the existing lifecycle/recovery matrix. Doc-test now validates the primary journey and executes the explicitly insecure journey.

Verification: mise run doc-test; mise run remote-smoke (dist/remote-acceptance/20260916T014135.904027000Z/report.json); mise run remote-smoke-vm (dist/remote-acceptance/vm-20260916T014649Z/report.json, retained owner-marked remote workspace); mise run lint; mise run format-check; mise run test; mise run typecheck. Regenerated docs/remote-demo.gif with disposable temporary credentials and inspected a rendered frame: it shows doctor, cross-client claim visibility, heartbeat, and release without invite or installation secrets. Independent verifier passed criteria 2-7 and identified missing client artifact-directory setup for criterion 1; added explicit owner-private directory creation on the administrator and client paths, then reran doc-test and all repository quality gates.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Delivered the guided remote onboarding journey in commit 3f1c4a8. The primary docs now use generated pinned TLS and self-contained invite artifacts without manual IDs, JSON parsing, or hand-edited YAML; advanced profile/bare-secret/metadata guidance remains separate. The local and two-host smoke harnesses prove guided setup, isolated admin/client state, artifact handoff, doctor, contention, visibility, heartbeat, release, and the existing recovery matrix. Verification passed: doc-test, remote-smoke, remote-smoke-vm, lint, format-check, test, typecheck, staged hooks, and independent acceptance verification after its one directory-setup finding was fixed. The regenerated GIF was inspected and contains no bearer secrets.
<!-- SECTION:FINAL_SUMMARY:END -->
