---
id: TASK-107.9
title: Route the CLI lifecycle and administration through the remote client
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies:
  - TASK-107.8
references:
  - internal/cli
  - internal/guard
  - internal/doctor
  - TASK-85.12
  - TASK-85.14
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 141000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
With the client library in place, every existing lifecycle command must behave identically against a remote profile, and the administrative actions the roles table grants need commands. The guarded `exec` supervisor stays on the client host, driving remote `BeginOperation`, `RenewOperation`, and `CompleteOperation` while the child runs locally; `replace-file` is disabled for remote profiles because its serialized replacement boundary is local only. Administrative commands cover invite issuance (the admin client generates the code and writes it to a file or hidden output), installation revocation by id, administrative claim revocation, reopening with an attestation record, bounded private inspection, and GC apply. `doctor` gains remote checks. Reopening is refused while any retained unresolved operation is unreconciled or the attestation is incomplete, and it records the completed lost-tail audit gap rather than blocking on it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `acquire`, `heartbeat`, `checkpoint`, `release`, same-host `transfer`, `status`, `list`, `events`, `history`, `watch`, `verify`, `inspect-operation`, and `reconcile` behave identically against a remote profile, keep handles and pending requests on the client host, and report the remote authority in `authorityId` and coordination scope.
- [ ] #2 `exec` under a remote profile runs the child locally, renews through the remote authority, terminates the process group and fails `ownership-lost` when renewal is not confirmed by the three-quarter mark, and leaves the operation `started`; `replace-file` under a remote profile fails with a distinct reason and performs no write.
- [ ] #3 `worklease admin invite --role ROLE --label LABEL` writes the generated code to a 0600 file or hidden output and never to argv or logs; `admin revoke-installation ID`, `admin revoke-claim`, `admin reopen`, `admin inspect`, and `admin gc` are role-checked and produce the audit events the design names.
- [ ] #4 `admin reopen` is refused while any retained unresolved operation is unreconciled or the attestation is incomplete; on success it atomically records the reopening, including any declared audit gap, and clears recovery mode.
- [ ] #5 `doctor` under a remote profile reports endpoint reachability, authority and incarnation match, credential presence without reading the secret, and recovery-mode state; `mise run ci` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
