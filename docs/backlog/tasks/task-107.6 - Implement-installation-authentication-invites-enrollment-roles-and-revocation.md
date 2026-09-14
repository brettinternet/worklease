---
id: TASK-107.6
title: >-
  Implement installation authentication: invites, enrollment, roles, and
  revocation
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:03'
labels:
  - remote-authority
dependencies:
  - TASK-107.4
references:
  - internal/lease/service.go
  - internal/store/schema.go
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 138000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement the service-layer primitives for invite issuance, bootstrap invite state, enrollment, installation roles, and revocation. Enrollment is the only invite-authenticated API operation. Invite codes contain at least 128 bits of entropy and have a short initial expiry. The client generates and durably stores its installation credential before dispatch; the authority stores only its hash and atomically burns the invite, creates an immutable installation identifier, and retains the bounded exact redemption result. An exact replay with the same credential returns the original result after invite expiry while replay remains retained for no more than 24 hours. A new redemption after expiry fails, and a burned code never enrolls again after replay GC. Human identity, OAuth, browser login, retirement, and a control plane remain outside the authority.

Authentication must be available inside the serialized mutation transaction, not only in HTTP middleware. The service looks up credential hashes with constant-time comparison and checks bearer identity and revocation, role, authority and restore incarnation, epoch credential where applicable, replay, and new-admission policy in the frozen order. A revoked retained installation overrides replay and returns `installation-revoked`; a credential whose row is absent returns `authentication-required`. A mutation serialized after revocation fails, while a mutation committed before revocation remains committed. Restore bootstrap enrollment is the sole enrollment exception while recovery mode is active.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The admin client generates and durably saves an invite code with at least 128 bits of entropy before dispatch. The authority receives and stores only its hash with role, non-unique label, issuing installation, request identity, short expiry, and replay provenance, and returns the same grant for an exact issuance retry without creating a second invite. No code appears in authority persistence or logs.
- [ ] #2 Enrollment first validates invite authentication, `authorityId`, and immutable `expectedRestoreId`, then atomically burns the invite and inserts one immutable installation id with its credential hash. Exact same-credential replay returns the original result after invite expiry while retained for no more than 24 hours; a different credential conflicts; a new redemption after expiry performs no burn or insert; and a burned code never enrolls again after replay GC.
- [ ] #3 The service exposes transaction-scoped authentication and role checks for TASK-107.4 and TASK-107.7. Credential-hash lookup uses constant-time comparison. A mutation serialized after revocation fails, a previously committed mutation remains committed, retained revoked installation state overrides replay with `installation-revoked`, and an absent credential fails `authentication-required` before authority, incarnation, epoch credential, replay, and admission checks.
- [ ] #4 `read`, `write`, and `admin` grants match the design. API administration covers invitation, installation and claim revocation, private inspection, GC, and reopening; offline init, restore, bootstrap reissue, and retirement are not API role grants.
- [ ] #5 Revocation is by immutable installation id, applies to the next request including pending exact replay, and preserves the installation's claims and unresolved operations. Rotation enrolls a new installation and revokes the old id.
- [ ] #6 Recovery mode closes ordinary enrollment and API invite issuance. Only the current offline bootstrap invite may enroll the new admin needed for inspection, recovery, and reopening.
- [ ] #7 No path or fixture places an invite code or installation credential in argv, logs, public events, or API errors.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
