---
id: TASK-107.6
title: >-
  Implement installation authentication: invites, enrollment, roles, and
  revocation
status: Done
assignee:
  - '@brett'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 05:39'
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
- [x] #1 The admin client generates and durably saves an invite code with at least 128 bits of entropy before dispatch. The authority receives and stores only its hash with role, non-unique label, issuing installation, request identity, short expiry, and replay provenance, and returns the same grant for an exact issuance retry without creating a second invite. No code appears in authority persistence or logs.
- [x] #2 Enrollment first validates invite authentication, `authorityId`, and immutable `expectedRestoreId`, then atomically burns the invite and inserts one immutable installation id with its credential hash. Exact same-credential replay returns the original result after invite expiry while retained for no more than 24 hours; a different credential conflicts; a new redemption after expiry performs no burn or insert; and a burned code never enrolls again after replay GC.
- [x] #3 The service exposes transaction-scoped authentication and role checks for TASK-107.4 and TASK-107.7. Credential-hash lookup uses constant-time comparison. A mutation serialized after revocation fails, a previously committed mutation remains committed, retained revoked installation state overrides replay with `installation-revoked`, and an absent credential fails `authentication-required` before authority, incarnation, epoch credential, replay, and admission checks.
- [x] #4 `read`, `write`, and `admin` grants match the design. API administration covers invitation, installation and claim revocation, private inspection, GC, and reopening; offline init, restore, bootstrap reissue, and retirement are not API role grants.
- [x] #5 Revocation is by immutable installation id, applies to the next request including pending exact replay, and preserves the installation's claims and unresolved operations. Rotation enrolls a new installation and revokes the old id.
- [x] #6 Recovery mode closes ordinary enrollment and API invite issuance. Only the current offline bootstrap invite may enroll the new admin needed for inspection, recovery, and reopening.
- [x] #7 No path or fixture places an invite code or installation credential in argv, logs, public events, or API errors.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect the frozen remote protocol, schema v2 authentication tables, and existing lease transaction boundaries.
2. Implement service-layer invite issuance, enrollment replay, transaction-scoped credential/role authentication, installation and claim revocation, and recovery restrictions using hashed secrets only.
3. Add focused tests for replay, expiry, ordering, revocation serialization, role grants, recovery bootstrap, redaction, and GC.
4. Run focused and repository quality gates, review the diff, fix findings, and record objective acceptance evidence.

5. Correct the frozen-protocol gaps found in review: add a store-owned verified v2 admin replay table with transactional completion for pre-extension v2 homes; use client-generated invite IDs; derive canonical installation identity from bearer authentication; apply authority time and protocol-domain replay hashing consistently; remove domain-owned DDL.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Review found that installation-revocation exact replay lacked a schema-v2 storage primitive and the first implementation created an unverified table from the lease domain. Corrective work will extend and verify schema v2 transactionally because the remote feature remains unshipped, then remove domain DDL. Review also found client-generated invite IDs, canonical bearer-derived identity, authority-time handling, and protocol replay-domain gaps.

Implemented service-layer authentication with client-side 256-bit secret generation, hash-only invite issuance, atomic enrollment and bounded replay, transaction-scoped bearer-derived role checks, installation/claim revocation, recovery bootstrap restrictions, and store-owned exact admin replay. Extended unshipped schema v2 transactionally for old writable homes while preserving read-only compatibility. Independent review found and fixed remote local-only replay-field acceptance and explicit-expiry timestamp drift. Validation: focused lease/store tests, go test ./..., mise run lint, format-check, typecheck, test, and mise run ci all passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented installation authentication, invite issuance/enrollment replay, role enforcement, revocation, and recovery bootstrap restrictions. Credentials remain client-generated and hash-only at rest; authenticated identity is re-derived inside each transaction before replay or mutation. Added verified schema-v2 admin replay completion plus expiry, recovery, revocation-ordering, clock, redaction, and compatibility regressions. Independent review findings were fixed, and mise run ci passed.

Delivery commit: 8131148 (Implement installation authentication).
<!-- SECTION:FINAL_SUMMARY:END -->
