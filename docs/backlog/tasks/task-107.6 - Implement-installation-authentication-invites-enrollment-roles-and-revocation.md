---
id: TASK-107.6
title: >-
  Implement installation authentication: invites, enrollment, roles, and
  revocation
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
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
Authority authentication is separate from claim credentials and is owned by Worklease with no OAuth server, browser login, or session. Enrollment is by invite. The admin client generates a one-shot code of at least 128 bits and sends only its hash, role, non-unique label, request id, and deadline; the redeemer generates and durably stores its installation credential before dispatch and redeems the code over TLS; the authority atomically burns the invite, inserts an immutable installation id and credential hash, and stores the redemption replay result. This applies the same rule the claim service already applies to claim tokens: the client saves its secret before dispatch and the authority stores only a hash.

Bearer lookup, role mapping, and revocation must be rechecked inside the serialized mutation transaction, not only in HTTP middleware, so a revocation racing a mutation wins. Human identity stays outside the authority: it knows installations, roles, invites, and issuers, not people. This task delivers the service-layer authentication logic and its typed requests; the `serve` task wires the routes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Invite issuance stores only the code hash with role, label, issuing installation, request id, and an expiry of about ten minutes; an identical retry returns the same grant without creating a second invite; the code is never persisted or logged.
- [ ] #2 Redemption validates `authorityId` and `expectedRestoreId`, then atomically burns the invite and inserts an immutable installation id with the credential hash; the same code with the same credential retried after a lost response returns the original result within a bounded window no longer than 24 h; a different credential against a burned code conflicts; an expired, reused, mismatched-incarnation, or revoked invite is refused with no burn and no insert.
- [ ] #3 Every authenticated request resolves the bearer by constant-time hash comparison to one installation and role; `read`, `write`, and `admin` grants match the roles table in the design; a `read` bearer cannot mutate and a `write` bearer cannot issue invites, revoke, reopen, or inspect private epochs it does not hold.
- [ ] #4 Revocation is by installation id, takes effect on the next request including pending exact replays with `installation-revoked`, does not force-release the installation claims, and is rechecked in the same transaction as the mutation; a bearer whose row is absent fails `authentication-required`.
- [ ] #5 Rotation is enrolling a new installation and revoking the old id; no code path, test, or fixture places an invite or installation credential on argv or in log output.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
