---
id: TASK-107.1
title: Freeze the remote protocol and administrative surface
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
labels:
  - remote-authority
dependencies: []
references:
  - internal/lease/service.go
  - internal/reason/reason.go
  - internal/watch/watch.go
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: spike
ordinal: 133000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The design defers freezing the wire protocol until implementation begins so the field mapping reflects the real typed requests in `internal/lease` rather than a guess. The server and client tasks both need one authoritative mapping before they start, or they will disagree on fields, envelopes, and error reasons.

Produce the protocol specification as a Backlog document. For each lifecycle method (acquire, heartbeat, checkpoint, release, same-host transfer, begin/renew/complete operation, inspect, reconcile, status, list, events, history, watch) and each administrative action (invite issuance, enroll, installation revocation, administrative claim revocation, reopening, bounded private inspection, GC apply) record the explicit allowed subset of request and result fields mapped field by field onto the Go types, the request envelope (`authorityId`, `expectedRestoreId`, protocol version), the response envelope (`restoreId`, `authorityTime`), error reasons with their HTTP mapping, body and response size limits, and the watch long-poll bound. Local compatibility fields such as `LegacyRequestHash` are excluded from client-selectable input. Then amend contract section 20 through the section 15 procedure to freeze the protocol by reference. Do not reserve URLs or version numbers anywhere else before this document exists.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A Backlog document specifies every route with request and response fields mapped field by field onto `internal/lease` request and result types, and names each field that is deliberately not exposed, including `LegacyRequestHash`.
- [ ] #2 The document enumerates `authority-restored`, `installation-revoked`, `authentication-required`, `resource-not-enrolled`, `unknown-outcome` on replay of a still-started operation, and the `restored` and `revoked` end reasons, each with its trigger and HTTP status mapping.
- [ ] #3 The document fixes the envelope rules: `authorityId` and `expectedRestoreId` on every authenticated request with only read-only metadata discovery allowed to omit the latter, `restoreId` and `authorityTime` on every application response, `restoreId` embedded in cursors, no resource keys or bearer credentials in URLs, non-cacheable responses, and the 30 s watch long-poll bound.
- [ ] #4 Contract section 17 gains an amendment entry freezing the protocol by reference to that document and TASK-85 carries the summary comment.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
