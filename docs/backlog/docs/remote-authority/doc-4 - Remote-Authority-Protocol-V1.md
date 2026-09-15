---
id: doc-4
title: Remote Authority Protocol V1
type: specification
created_date: '2026-09-14 01:19'
updated_date: '2026-09-15 02:17'
tags:
  - remote-authority
  - protocol
  - v1
---
# Remote Authority Protocol V1

Status: frozen specification for TASK-107.1. This document defines the initial
remote wire and command surface. It does not claim that the surface is
implemented or shipped. Normative words such as MUST describe the later
TASK-107 implementation.

## 1. Scope and ownership

The protocol is storage-neutral and binds the existing Go domain services over
HTTPS. SQLite table, column, transaction, and row shapes are not wire types.

Implementation ownership is fixed as follows:

- TASK-107.2 owns schema version 2, persisted replay/incarnation provenance,
  installations, invites, recovery state, admitted limits, and migrations.
- TASK-107.4 owns domain validation, mutation ordering, exact replay,
  incarnation checks, admission limits, recovery-mode behavior, administrative
  claim revocation, and reopening invariants.
- TASK-107.5 owns offline `hosted` command composition, hosted lock use,
  crash-recoverable bootstrap secret files, restore, reissue, and retirement.
- TASK-107.6 owns invite and installation credential generation, hashing,
  authentication, roles, enrollment, rotation-by-reenrollment, and revocation.
- TASK-107.7 owns HTTP parsing, envelopes, limits, rate limiting, cancellation,
  logging redaction, and `serve`.
- TASK-107.8 owns profiles, trusted endpoints, installation credential storage,
  authority-time estimation, redirects, and durable pending requests.
- TASK-107.9 owns CLI routing through the local or remote client.
- TASK-107.10 owns the existing local stdio MCP adapter over that client.

No wire field exposes a SQLite primary key, schema/row version, table, row ID,
transaction ID, backup path, or lock path. Domain claim revisions and recovery
revisions remain explicit typed fields.

## 2. Transport, version, and envelopes

The protocol identifier is the exact ASCII string `worklease-http/1`. All
requests use HTTP/1.1 or HTTP/2 over HTTPS, except when
`--allow-insecure-http` explicitly permits cleartext HTTP. Credential-bearing
redirects are never
followed. There is no version negotiation by URL.

Every request sends:

```text
Worklease-Protocol-Version: worklease-http/1
Accept: application/json
Content-Type: application/json; charset=utf-8   # requests with bodies
```

A missing or unsupported protocol header returns HTTP 426 and an error envelope
whose `details.supportedProtocolVersions` is exactly `["worklease-http/1"]`.
The metadata result also reports that list. V1 accepts no version range.

Every Worklease application response, including errors and exact replay, is one
of these JSON objects:

```json
{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"<32 lowercase hex>","restoreId":"<32 lowercase hex>","authorityTime":"<RFC3339Nano UTC>","result":{}}
{"ok":false,"protocolVersion":"worklease-http/1","authorityId":"<32 lowercase hex>","restoreId":"<32 lowercase hex>","authorityTime":"<RFC3339Nano UTC>","error":{"reason":"<stable reason>","message":"<redacted text>","details":{}}}
```

`result` and `error` are mutually exclusive. `details` is optional and must be
an object. `authorityTime` is freshly sampled when the envelope is written;
historical replay content remains nested under `result`. HTTP status is not a
substitute for the reason. Every response, including health and metadata, sends
`Cache-Control: no-store, max-age=0`, `Pragma: no-cache`, and
`X-Content-Type-Options: nosniff`, and never an `ETag` or `Last-Modified`
header. Clients reject a successful response with the
wrong protocol, authority, incarnation, media type, or envelope shape.

JSON uses UTF-8, lower-camel-case field names, RFC3339Nano UTC timestamps, and
integer microseconds for durations (`ttlMicros`, `waitMicros`,
`pollIntervalMicros`, `maxHoldMicros`). Sequence positions remain decimal
strings. IDs and SHA-256 values remain lowercase hex. Omitted optional fields
are different from explicit zero values where the typed request distinguishes
them. Unknown fields, duplicate object keys, trailing JSON, non-integral
numbers, invalid UTF-8, and more than one JSON value are `invalid-argument`.
Request hashing uses the existing typed service canonical intent after defaults,
not raw JSON member order. V1 therefore does not assign semantic meaning to JSON
member order.

## 3. Authentication headers and bounded input

The only unauthenticated routes are `GET /healthz` and
`GET /.well-known/worklease`. `POST /v1/enroll` uses:

```text
Authorization: Invite <invite bearer>
Worklease-New-Installation-Authorization: Bearer <client-generated credential>
```

Every other `/v1/` route uses:

```text
Authorization: Bearer <installation credential>
```

A route that authenticates an existing claim epoch additionally uses
`Worklease-Claim-Authorization: Bearer <claim credential>`. Acquire uses
`Worklease-New-Claim-Authorization`; same-host transfer uses
`Worklease-New-Claim-Authorization` for the successor. Credentials never occur
in URLs, JSON bodies, responses, logs, cursors, events, or error details.

The server reads at most 1 MiB of request body and writes at most 4 MiB of
response body, measured as encoded bytes. It rejects a declared or observed
larger request with 413 before JSON decoding. If a typed result would exceed 4
MiB, it returns `response-too-large` with 507 and no partial result. Health is at
most 1 KiB, metadata at most 4 KiB, and enrollment at most 16 KiB. All three are
rate-limited per deployment-configured source bucket; exhaustion is
`rate-limited` with 429 and integer `Retry-After` seconds. The limits do not
weaken narrower domain bounds: 1-32 resources; 8 KiB canonical checkpoint; 8
KiB reconciliation evidence; ledger page limit 1-1000; 4 KiB decoded cursor;
TTL 1 second-1 hour; request replay deadline no more than 24 hours; CLI wait no
more than 60 seconds; and remote watch timeout exactly 0-30 seconds, default 30.

Client disconnect, request-context cancellation, or server shutdown cancels
parsing, read polling, and any transaction waiting to begin. A watch returns
`cancelled` only when its response can still be written; otherwise the client
classifies the attempt as uncertain. Cancellation before a serialized mutation
commits is a definitive no-commit only when the service proves rollback.
Cancellation at or after dispatch/commit remains an unknown outcome and the
client retains its exact pending request. The server never keeps a storage
transaction open while waiting or writing a response.

## 4. Common wire types

These named V1 wire types map typed Go values without making the Go structs
serialization APIs.

### 4.1 Request context

`AuthenticatedContextV1` is embedded in every installation-authenticated body:

```text
protocolVersion string                 # must equal worklease-http/1
authorityId string                      # expected immutable authority
expectedRestoreId string                # immutable request incarnation
```

Mutations also carry `requestNotAfter` and the operation ID required by the
mapped service request. Read requests still carry authority and restore
binding. Initial metadata discovery is the only operation that may omit
`expectedRestoreId`. Enrollment carries it explicitly after discovery.

`ClaimCredentialsV1` maps `lease.Credentials`: `authorityId` is in the common
context; `claimId` and `revision` are body fields; token is exclusively the
claim authorization header. Revision is required on mutations and optional on
read-only verify where the existing typed contract permits it.

### 4.2 Lease projections

`GrantV1` maps `lease.Grant`: `claimId`, `resources`, `agentId`, `sessionId`,
`workKey`, `revision`, `acquiredAt`, `expiresAt`, `guarantee`, `authorityId`,
`active`, `receipt`, `recovery`, and `unknownOperations`. It deliberately omits
`localReplaceAllowed`: remote replacement is unsupported and the value is
always false at the remote boundary.

`RecoveryV1` maps `lease.Recovery`: `resource`, `claimId`,
`checkpointPresent`, optional `checkpoint`. `ClaimViewV1` maps
`lease.ClaimView`: all fields except `localReplaceAllowed`, retaining
`checkpointPresent` and `unknownOperations`. `ResourceStatusV1` maps
`lease.ResourceStatus`: `resource`, `state`, optional `claim`.

`ReceiptV1` maps `lease.Receipt`: `operationId`, `claimId`, `kind`,
`requestSha256`, `revision`, `idempotent`, `committed`, optional `result`.
`VerificationV1` maps `lease.Verification` as `claim` and
`unknownOperations`.

### 4.3 Ledger, watch, and retention projections

`EventV1`, `EventsPageV1`, `OperationV1`, `EpochV1`, `HistoryCoverageV1`, and
`HistoryPageV1` map the same lower-camel fields from `internal/ledger`.
`OperationV1` is public by default; private mode may additionally return
`expectedRevision`, `requestNotAfter`, `receipt`, `evidence`, and `outcome`.
`CursorV1` extends the existing opaque cursor payload with `restoreId`; V1
validates protocol cursor version, `authorityId`, `restoreId`, feed, exact
filter, and decimal-string sequence before opening storage. An old-incarnation
cursor is `authority-restored`, not `cursor-invalid`.

`WatchResultV1` maps `watch.Result`: `authorityId`, `cursor`, `nextCursor`,
optional `event`, `timedOut`, `gap`, optional `resetCursor`, `free`, `changed`,
`resources`, `unresolvedPredecessor`, and `unresolvedOperations`. The outer
envelope adds current `restoreId` and `authorityTime`.

`GCResultV1` maps `gc.Result`: `dryRun`, `capturedAt`, `cutoff`, optional
`retentionDays`, `eligible`, `protected`, optional `retired`, optional
`collected`, `prunedThroughSequence`, and `lastEventSequence`.

### 4.4 New authentication and administration types

These are named typed extensions owned by the tasks in section 1:

```text
MetadataResultV1 { authorityId, restoreId, supportedProtocolVersions[], authorityTime }
EnrollRequestV1 { protocolVersion, authorityId, expectedRestoreId, requestId,
                  requestNotAfter, installationId, label }
EnrollResultV1 { installationId, role, label, enrolledAt }
IssueInviteRequestV1 { AuthenticatedContextV1, operationId, requestNotAfter,
                       inviteId, role, label, expiresAt?, inviteSha256 }
IssueInviteResultV1 { inviteId, role, label, expiresAt, issuedAt, issuedByInstallationId }
InstallationViewV1 { installationId, role, label, enrolledAt, revokedAt?,
                     revokedByInstallationId? }
RevokeInstallationRequestV1 { AuthenticatedContextV1, operationId,
                              requestNotAfter, installationId, reason }
RevokeClaimRequestV1 { AuthenticatedContextV1, operationId, requestNotAfter,
                       claimId, reason }
RecoveryStatusResultV1 { recoveryMode, restoredAt?, selectedDurableCutoff?,
                         lossIntervalStart?, lossIntervalEnd?, cutoffKnown,
                         unresolvedOperations[], retainedInstallations[] }
ReopenRequestV1 { AuthenticatedContextV1, operationId, requestNotAfter,
                  expectedRecoveryRevision, attestation }
ReopenAttestationV1 { inventoryComplete, pendingSetsComplete,
                      retainedOutcomesComplete, namespaceCessationEstablished,
                      evidenceReferences[] }
ReopenResultV1 { recoveryMode, recoveryRevision, reopenedAt,
                 reopenedByInstallationId }
```

Labels and reasons are trimmed UTF-8 public text, maximum 1024 bytes. Evidence
references are private strings, maximum 2048 bytes each, at most 64 entries;
the canonical attestation is at most 128 KiB. An omitted invite `expiresAt`
uses authority time plus 15 minutes; an explicit expiry must be from 1 minute
to 24 hours after authority time. The authority persists and replays the
resolved expiry without changing the exact omitted-input identity.
`inviteSha256` is the hash of the client-generated invite from the private
invite file. The plaintext invite is
never returned. Issuance succeeds only after the client has durably saved it.
Installation and invite IDs are 32 lowercase hex. Roles are exactly `read`,
`write`, or `admin`.

## 5. Exact routes and field mappings

All `/v1/` reads use POST so opaque keys and cursors do not enter URLs or access
logs. Every installation-authenticated request embeds
`AuthenticatedContextV1`; enrollment embeds the authority/incarnation subset
shown in `EnrollRequestV1`. No unlisted route, method, query parameter, or
request field is part of V1.

### 5.1 Discovery and authentication

| Method and route | Authentication | Request fields | Result |
| --- | --- | --- | --- |
| `GET /healthz` | none | no body or query | `{"ok":true}` only; liveness, not authority readiness |
| `GET /.well-known/worklease` | none | no body or query; version header required | `MetadataResultV1` |
| `POST /v1/enroll` | invite plus new-installation headers | `EnrollRequestV1` | `EnrollResultV1` |

Enrollment validates invite authentication, revocation/burn state, role,
`authorityId`, and immutable `expectedRestoreId` in one serialized redemption.
Exact retry is keyed by `requestId`, invite, installation ID, credential hash,
and exact normalized body. Same request returns the original result within its
retention window; changed credential or body is `operation-request-mismatch`.
A revoked installation result overrides redemption replay. In recovery mode only
the current offline bootstrap admin invite may enroll.

### 5.2 Claims and lifecycle

| Route | Typed mapping and exact body fields | Result |
| --- | --- | --- |
| `POST /v1/claims/acquire` | `lease.AcquireRequest`: common context; `claimId`, `resources`, `agentId`, `sessionId`, `workKey`, `ttlMicros`, `requestNotAfter`, `coordinationOnly`, `maxHoldMicros`. New token is the header. `authorityId` maps from context. | `GrantV1` |
| `POST /v1/claims/status` | `lease.Selector`: common context; exactly one of `claimId` or `resources`. | `{claim?,claims[],resources[]}` from `lease.Status` |
| `POST /v1/claims/list` | common context; optional `resource`. | `{claims: ClaimViewV1[]}` from `lease.List` |
| `POST /v1/claims/heartbeat` | `lease.Credentials` + `lease.Renew`: common context; `claimId`, `revision`, `operationId`, `ttlMicros`, `requestNotAfter`. | `ReceiptV1` |
| `POST /v1/claims/checkpoint` | `lease.Credentials` + `lease.CheckpointRequest`: common context; `claimId`, `revision`, `operationId`, `ttlMicros`, `data`, `requestNotAfter`. | `ReceiptV1` |
| `POST /v1/claims/release` | `lease.Credentials` + `lease.ReleaseRequest`: common context; `claimId`, `revision`, `operationId`, `reason`, `requestNotAfter`. | `ReceiptV1` |
| `POST /v1/claims/transfer` | `lease.Credentials` + `lease.TransferRequest`: common context; predecessor `claimId`, `revision`, `operationId`, `successorClaimId`, `toAgent`, `toSession`, `toWorkKey`, `ttlMicros`, `requestNotAfter`. Successor token is the new-claim header. Same installation only. | `GrantV1` |
| `POST /v1/claims/verify` | `lease.Credentials` + `lease.Service.Verify`: common context; `claimId`, optional `revision`, optional `expectedResources`. | `VerificationV1` |

Status, list, and verify require `read`; acquire, heartbeat, checkpoint, release,
transfer, and every operation-lifecycle route require `write`. A higher role
includes lower-role grants.

Remote acquire never accepts `waitMicros`, `pollIntervalMicros`,
`localReplaceAllowed`, `holdUntil`, or `LegacyRequestHash`. Waiting is a client
loop using watch and repeated exact acquire attempts; `maxHoldMicros` is new
admission input persisted as an absolute authority-time ceiling. Heartbeat and
checkpoint derive that persisted ceiling and never accept `HoldUntil` or
`LegacyRequestHash`. Transfer is same-installation only and inherits the
predecessor hold deadline; cross-host transfer is absent.

### 5.3 Guard operation lifecycle and reconciliation

| Route | Typed mapping and exact body fields | Result |
| --- | --- | --- |
| `POST /v1/operations/begin` | `lease.Credentials` + `lease.OperationIntent`: common context; `claimId`, `revision`, `operationId`, `kind`, `request`, optional `requestSha256`, `requestNotAfter`, `ttlMicros`. `kind` is `exec` only remotely. | `lease.Started`: `operationId`, `claimId`, `kind`, `revision`, `requestSha256`, `completed`, optional `receipt` |
| `POST /v1/operations/renew` | `RemoteOperationRenewRequestV1`, extending `lease.RenewOperation`: common context; `claimId`, `revision`, `renewalId`, target `operationId`, `ttlMicros`, `requestNotAfter`. | `ReceiptV1` |
| `POST /v1/operations/complete` | `lease.CompleteOperation`: common context; `claimId`, `revision`, `operationId`, `requestNotAfter`, `receipt`. | `ReceiptV1` |
| `POST /v1/operations/inspect` | `ledger.InspectRequest`: common context; `operationId`, optional `claimId` or `resource`, `view` (`public` or `private`). Claim header required for private epoch view unless caller is admin. | `OperationV1` |
| `POST /v1/operations/reconcile` | `lease.Credentials` + `lease.ReconcileRequest`: common context; resolver `claimId`, `revision`, `operationId`, `targetClaimId`, `targetOperationId`, `expectedRequestSha256`, `outcome`, `evidence`, `ttlMicros`, `requestNotAfter`. | `lease.ReconciliationReceipt` public fields: `operationId`, `targetClaimId`, `targetOperationId`, `resolverClaimId`, `outcome`, `requestSha256`, `revision`, `reconciledAt`, `expiresAt`, `idempotent`, `committed` |

The canonical begin `request` is at most 48 KiB. TASK-107.8 introduces a
`RemoteHandleV2` file with a 1 MiB encoded-file limit; before begin, the client
constructs the complete pending handle, including all resources and the exact
request, and rejects it unless that final encoded file fits. Local handle V1
keeps its existing 64 KiB limit. A completion `receipt` is at most 512 KiB as
final canonical encoded JSON, not as a sum of unescaped component bytes. Remote
exec retains the existing total stdout/stderr byte counts but truncates each
JSON-encoded string value to at most 192 KiB at a valid UTF-8 boundary, setting
the existing truncation booleans. It then constructs the full receipt; if
metadata and encoded strings still exceed 512 KiB, it deterministically removes
trailing code points from stderr first and stdout second until the complete
canonical receipt fits. This is a remote-client projection and does not change
local exec's 1 MiB-per-stream capture. The bounded completion request is durably
saved before it is sent, so every successfully started local effect has a
representable completion. These values are canonical strict JSON. They may
contain private argv intent or child result data. They are never placed in
public inspection, events, logs, or errors, and the server never executes them.
Conformance covers a 32-resource maximum-size pending handle and
quote/control-heavy stdout and stderr so preflight and completion remain
deterministic. `replace-file` is rejected as `operation-kind-unsupported`.
`ReplayOperation` and `RunGuardedOperation` are internal composition helpers,
not routes; retrying `begin` with identical intent performs retained replay.
`ReconcileAtCurrentRevision` is a client recovery choice mapped through the same
reconcile route, not a second wire method.

`RemoteOperationRenewRequestV1` is a named typed extension: `renewalId` is a
fresh 32-lowercase-hex exact-replay key for one renewal of the target started
operation. TASK-107.2 retains its normalized request and receipt through
`requestNotAfter`; TASK-107.4 resolves identical replay before current revision
authorization, returns the original receipt without extending again, and
returns `operation-request-mismatch` for changed intent. A different renewal
uses a new `renewalId`. This makes a lost renewal response recoverable even when
the caller still holds the pre-renewal revision.

### 5.4 Reads, cursors, watch, and GC

| Route | Typed mapping and exact body fields | Result |
| --- | --- | --- |
| `POST /v1/events` | `ledger.Events`: common context; optional `cursor`, optional `limit`. | `EventsPageV1` |
| `POST /v1/history` | `ledger.History`: common context; optional `resource`, optional `cursor`, optional `limit`, optional `full`. With no resource this is events. `full` remains redacted unless admin private inspection is used separately. | `HistoryPageV1` or `EventsPageV1` |
| `POST /v1/watch` | `watch.Request`: common context; optional `cursor`, `resources`, optional `until` (`free` or `change`), optional `timeoutMicros` (0-30000000). Poll interval and clock are server-owned. | `WatchResultV1` |
| `POST /v1/admin/gc` | Admin-only `gc.Request`: common context; `operationId`, `requestNotAfter`; exactly one of `cutoff` or `retentionDays`; `apply` must be true. | `GCResultV1` |

All reads require a valid installation and incarnation. Watch is one long poll,
default and maximum 30 seconds. The client reconnects with jitter and the
returned cursor. Server cancellation is bounded by the request context; no
transaction is held while polling. Cursors are incarnation-bound. A gap returns
normally with `gap:true` and a reset cursor; it never silently resumes.
HTTP V1 exposes GC apply only. In remote mode, CLI `gc` without `--apply`
returns a capability error rather than synthesizing a local preview.

### 5.5 Administrative routes

| Route | Role | Exact body fields | Result |
| --- | --- | --- | --- |
| `POST /v1/admin/invites/issue` | admin | `IssueInviteRequestV1` | `IssueInviteResultV1` |
| `POST /v1/admin/installations/list` | admin | common context; optional `includeRevoked` | `{installations: InstallationViewV1[]}` |
| `POST /v1/admin/installations/revoke` | admin | `RevokeInstallationRequestV1` | `{installationId,revokedAt,revokedByInstallationId}` |
| `POST /v1/admin/claims/revoke` | admin | `RevokeClaimRequestV1` | `ReceiptV1`, end reason `revoked` |
| `POST /v1/admin/recovery/status` | admin | common context | `RecoveryStatusResultV1` |
| `POST /v1/admin/recovery/reopen` | admin | `ReopenRequestV1` | `ReopenResultV1` |

There is no invite plaintext response, retirement route, recovery-import route,
backend maintenance route, configuration route, profile route, server-side
execution route, remote replacement route, or cross-host transfer route.
Administrative mutations use exact replay and durable client pending state.

## 6. Serialized mutation order and replay

Every installation-authenticated mutation executes this order inside the one
namespace serialized boundary:

1. look up the installation credential hash; an absent row returns
   `authentication-required`;
2. reject a retained revoked installation as `installation-revoked`;
3. verify the route role (`read`, `write`, or `admin`);
4. verify `authorityId`, returning `authority-mismatch` for the wrong authority;
   then verify immutable `expectedRestoreId`, returning `authority-restored`
   with current envelope identity only for an incarnation mismatch;
5. authenticate the original claim epoch credential when the operation is
   epoch-scoped, including replay of an ended epoch;
6. find retained operation/redemption replay and compare the exact normalized
   request, original incarnation, credential hashes, and deadline;
7. return a completed replay result, or `unknown-outcome` for a retained started
   operation, before evaluating policy changes;
8. only for a new request, enforce recovery mode, admitted prefix, resource
   cardinality, TTL/hold bounds, and other domain admission checks;
9. mutate domain state, replay record, projections, installation attribution,
   and event atomically.

HTTP middleware may reject malformed syntax, oversized input, and obviously
missing headers, but it never substitutes for transactional steps 1-9. Current
absence and revocation take precedence over role, incarnation, claim credential,
replay, and admission. For a present non-revoked installation, authorization
precedes incarnation. Revocation racing a mutation follows transaction order and
is not retroactive. Exact replay survives later prefix/bound changes and
recovery mode, but not current installation revocation. Enrollment follows its
separate invite order in section 5.1.

Mutation replay identity includes protocol version, authority ID,
`expectedRestoreId`, authenticated installation ID, operation/request ID, all
normalized typed input, generated IDs, and the relevant new or existing
credential hash. A timeout after dispatch never permits a new operation ID.
`requestNotAfter` is immutable and at most 24 hours after authority time.
Operation renewal replay uses its `renewalId` and is resolved before stale
revision checks. Completed replay is token-free and freshly enveloped. Retained started begin
returns `unknown-outcome`, never permission to execute again. A changed request
returns `operation-request-mismatch`; expired retained replay returns
`replay-expired`. GC never removes replay or authentication state before its
request deadline or while needed by unresolved recovery.

## 7. Error to HTTP mapping

Every failure uses the error envelope. Messages and details are redacted and
bounded to 4 KiB and 16 KiB respectively. The following mapping is exhaustive
for V1; future reasons require a protocol amendment.

| HTTP | Reasons |
| --- | --- |
| 400 | `invalid-argument`, `config-invalid`, `config-missing`, `claim-selection-missing`, `resource-input-conflict`, `invalid-resource`, `unknown-policy`, `invalid-path`, `cursor-invalid`, `hook-input-invalid`, `operation-kind-unsupported` |
| 401 | `authentication-required`, `installation-revoked`, `invalid-token`, `invite-invalid`, `invite-expired`, `credential-unsafe`, `credential-malformed`, `credential-source-conflict` |
| 403 | `authorization-denied`, `resource-not-enrolled`, `unsupported-coordination-replace` |
| 404 | `operation-not-found` |
| 408 | `cancelled`, `interrupted` |
| 409 | `already-claimed`, `stale-claim`, `stale-revision`, `claim-expired`, `verify-failed`, `ownership-lost`, `handle-in-use`, `operation-in-progress`, `unknown-outcome-pending`, `operation-request-mismatch`, `expected-hash-mismatch`, `reconciliation-conflict`, `authority-mismatch`, `invite-used` |
| 410 | `replay-expired` |
| 413 | `request-too-large` |
| 422 | `unknown-outcome`, `operation-ambiguous`, `authority-restored`, `recovery-required`, `recovery-closed` |
| 426 | `protocol-version-unsupported`, `schema-unsupported` |
| 429 | `rate-limited`, `wait-timeout` |
| 500 | `internal` |
| 503 | `storage-failure`, `schema-corrupt`, `home-unsafe`, `clock-regression` |
| 507 | `response-too-large` |

Local/client-only reasons `handle-write-failed`, `handle-unsafe`,
`handle-malformed`, `agent-id-required`, `setup-config-malformed`, and
`child-timeout` are never emitted by the server. If received they are treated as
an invalid server response. Transport cancellation before any response is a
client transport error, not a fabricated server reason. 5xx and transport
failure after dispatch are not definitive no-commit evidence.

For contention, `already-claimed` may include only the redacted current
`ClaimViewV1` holder metadata permitted by normal status. `authority-restored`
may include only current `restoreId`. `protocol-version-unsupported` includes
only supported versions. Authentication failures do not reveal whether an
installation ID, invite, claim, or operation exists beyond the distinct
retained-revoked rule.

## 8. CLI and configuration flags

The standard binary remains inert unless a profile or server/offline command is
selected.

### 8.1 Server and offline hosted commands

```text
worklease serve --server-config FILE [--allow-insecure-http]
worklease hosted init --home DIR --server-config FILE --bootstrap-invite-file FILE
worklease hosted restore --home DIR --from FILE --selected-cutoff RFC3339 \
  --loss-interval-start RFC3339 --loss-interval-end RFC3339 \
  --bootstrap-invite-file FILE [--cutoff-unknown]
worklease hosted bootstrap-reissue --home DIR --bootstrap-invite-file FILE
worklease hosted retire --home DIR [--force --unresolved-export FILE]
```

`serve` takes listen address, TLS/proxy policy, admitted prefixes, TTL/hold
bounds, rate limits, and database home only from `--server-config`; no individual
network/admission flag overrides it except the explicit
`--allow-insecure-http` transport opt-in, which permits cleartext HTTP on the
configured listen address. Hosted commands are offline-only, take the hosted
lock, and never use a remote profile. `retire --force` requires the redacted unresolved export path.
There is no `serve --daemon`, hot reload, retirement HTTP call, or schema/backend
selection flag in V1.

### 8.2 Profiles, enrollment, and administration

```text
worklease profile add NAME --endpoint URL --authority-id ID [--allow-insecure-http]
worklease profile list
worklease profile show NAME
worklease profile remove NAME
worklease profile default NAME
worklease profile bind NAME [--cwd DIR]
worklease profile unbind [--cwd DIR]
worklease enroll --profile NAME (--invite-file FILE | --invite-fd N) \
  [--credential-file FILE | --credential-fd N] [--label TEXT]
worklease invite issue --profile NAME --role read|write|admin \
  (--invite-file FILE | --invite-fd N) [--label TEXT] [--expires-at RFC3339]
worklease installation list --profile NAME [--include-revoked]
worklease installation revoke --profile NAME --installation-id ID [--reason TEXT]
worklease claim revoke --profile NAME --claim-id ID [--reason TEXT]
worklease recovery status --profile NAME
worklease recovery reopen --profile NAME --expected-recovery-revision N \
  --attestation-file FILE
```

Invite issuance writes a client-generated plaintext invite durably to the
selected file/descriptor source before dispatch and sends only its hash.
Enrollment reads an invite from hidden prompt, file, or descriptor; no invite or
installation bearer is accepted on argv. `profile add` performs bounded metadata
discovery and verifies the supplied authority before saving. Endpoint changes
require remove/add; credential-bearing redirects are refused. Profile selection
for existing CLI lifecycle/read commands is exactly `--profile`, then
`WORKLEASE_PROFILE`, then user-side project binding, then user default, then
local. `--local` is the explicit override and conflicts with `--profile` or the
environment selection. Configured remote failure never falls back locally.

Existing lifecycle flags keep their names and local meanings. In remote mode the
client rejects local-only `--path`/`backlog-md`/`markdown` keys at admission,
`replace-file`, cross-installation `transfer`, and any explicit `--poll-interval`
sent to the server. `--wait` remains client-side and is capped at 60 seconds.
All remote mutations, including `--no-handle`, create durable pending state.

## 9. Operation exposure matrix

`HTTP` means the exact route in section 5. `MCP` means an existing stdio tool,
not a remote MCP endpoint.

| Operation | Offline only | Client-local | HTTP | CLI | Existing MCP |
| --- | ---: | ---: | ---: | ---: | ---: |
| health / metadata | no | discovery client | yes | profile/enroll internals | no |
| enroll | no | credential/profile finalization | yes | `enroll` | no |
| key derivation | no | yes | no | `key` | `key` |
| acquire | no | pending state / wait loop | yes | `acquire` | `acquire` |
| status / list | no | projection | yes | `status`, `list` | `status`, `list` |
| heartbeat / checkpoint / verify / release | no | handles and pending state | yes | same names | same names |
| same-host transfer | no | both handles | yes | `transfer` | no |
| cross-host transfer | excluded | excluded | no | no | no |
| begin / renew / complete guarded exec | no | process supervision and effect | yes | `exec` composition | no direct tools |
| server-side effect execution | excluded | effect stays local | no | no | no |
| replace-file | no | local authority only | no | local `replace-file` | no |
| events / history / watch | no | cursor persistence/backoff | yes | same names | `events`, `watch`; no `history` tool |
| public/private operation inspect | no | credential selection | yes | `op inspect` | no |
| reconcile | no | evidence collection | yes | `op reconcile` | no |
| invite issue | no | secret generation/storage | yes | `invite issue` | no |
| installation list/revoke | no | credential store cleanup after success | yes | `installation` commands | no |
| administrative claim revoke | no | no | yes | `claim revoke` | no |
| GC apply | no | no | yes | `gc --apply` | no |
| restore recovery status/reopen | no | attestation preparation | yes | `recovery` commands | no |
| initialize hosted authority | yes | no | no | `hosted init` | no |
| restore hosted authority | yes | no | no | `hosted restore` | no |
| bootstrap reissue | yes | no | no | `hosted bootstrap-reissue` | no |
| retirement | yes | no | no | `hosted retire` | no |
| recovery import | excluded | excluded | no | no | no |
| profile add/list/show/remove/default/bind | no | yes | metadata only for add | `profile` | no |
| instructions / setup / doctor | no | yes | no | existing commands | `instructions` only |

The remote authority is never an MCP endpoint. The local `worklease mcp` process
uses the same client and profile resolver. Existing MCP tool names remain
`key`, `acquire`, `status`, `list`, `heartbeat`, `checkpoint`, `verify`,
`watch`, `events`, `release`, and `instructions`; no enrollment,
administration, transfer, exec, replace, reconciliation, history, profile, or
recovery tool is added by TASK-107.

## 10. Security and logging invariants

Handlers log method, route template, status, duration, protocol version, and
redacted installation ID only. They never log headers, raw URLs, request/response
bodies, resources, cursors, credentials, hashes of bearer credentials, private
operation intent, receipts, checkpoints, reconciliation evidence, invite
labels, or reopening evidence. Unknown routes do not echo paths. Reverse-proxy
identity headers are ignored for authorization.

Every read and cursor call is installation-authenticated and incarnation-bound.
The words `public`, `redacted`, and `full` describe projection visibility inside
the authorized namespace, never unauthenticated access. Health proves only that
the process can answer HTTP. Metadata reveals only `authorityId`, `restoreId`,
supported protocol versions, and `authorityTime`.

This V1 surface explicitly excludes remote provider execution, remote
`replace-file`, retirement over HTTP, enrollment through MCP, recovery import,
cross-host transfer, browser/OAuth login, repository enrollment, hot
configuration, multi-namespace serving, and backend selection.
