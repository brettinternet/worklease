---
id: TASK-107.1
title: Freeze the remote protocol and administrative surface
status: done
type: spike
priority: high
assignees: ['@brett']
labels: [remote-authority]
parent: TASK-107
rank: ad4e25b9efd4e24fe
refs: [internal/lease/service.go, internal/reason/reason.go, internal/watch/watch.go, docs/remote-claim-authority.md, docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md, docs/backlog/docs/remote-authority/doc-4 - Remote-Authority-Protocol-V1.md]
files: [docs/backlog/docs/remote-authority/doc-4 - Remote-Authority-Protocol-V1.md, docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md, docs/backlog/tasks/task-85 - Rewrite-Worklease-in-Go.md, docs/backlog/tasks/task-107.1 - Freeze-the-remote-protocol-and-administrative-surface.md, docs/backlog/tasks/task-107.7 - Implement-worklease-serve.md]
created: 2026-09-14T00:37:00Z
updated: 2026-09-14T01:54:00Z
---

Freeze one authoritative remote protocol specification before client or server implementation. Map the actual typed requests and results in `internal/lease`, plus the required typed administrative extensions, into explicit wire fields. The specification owns the exact routes, flags, protocol version, envelopes, error mapping, cancellation behavior, size limits, replay rules, and serialization order. It must not expose SQLite details or reserve wire names elsewhere before this task settles them.

The specification distinguishes bounded unauthenticated metadata discovery and health from invite-authenticated enrollment and installation-authenticated API calls. Metadata discovery exposes only `authorityId`, `restoreId`, supported protocol version information, and authority time. Enrollment authenticates with the invite and carries `authorityId` plus immutable `expectedRestoreId`. Every other API route, including reads and cursor use, requires an installation bearer and incarnation binding. For mutations, the serialized order is authenticated installation and role, authority and restore incarnation, epoch credential where applicable, retained exact replay, then new-admission checks.

Enumerate which operations are offline-only, client-local, HTTP, CLI-only, or exposed through the existing MCP adapter so the protocol cannot imply remote provider execution, a retirement route, enrollment through MCP, or remote `replace-file`. Assign data and replay ownership to TASK-107.2, domain and admission ownership to TASK-107.4, offline lifecycle ownership to TASK-107.5, and authentication ownership to TASK-107.6. Then freeze the specification by reference through the section 15 amendment procedure and record the required TASK-85 comment.

## Acceptance criteria

- [x] A Backlog protocol document maps every supported lifecycle, read, watch, authentication, and administrative request and result field onto real `internal/lease` types or named typed extensions, and identifies excluded local compatibility fields including `LegacyRequestHash`.
- [x] The document fixes exact routes and flags, protocol and version handling, request and response size bounds, cancellation behavior, non-cacheable responses, the 30-second watch long poll, cursor incarnation binding, and every error-to-HTTP mapping including unsupported version, authentication, authorization, cancellation, conflict, recovery, and storage failures.
- [x] The unauthenticated surface is limited to health and bounded identity-only metadata discovery. Enrollment uses invite authentication with `authorityId` and immutable `expectedRestoreId`; every other route, including reads and cursors, requires a valid installation bearer and incarnation check.
- [x] The document fixes mutation ordering inside the serialized boundary as installation and role, authority and incarnation, epoch credential where applicable, retained exact replay, then new-admission checks, with revoked and absent credentials taking precedence as specified.
- [x] A matrix identifies offline-only, client-local, HTTP, CLI-only, and existing MCP operations. It excludes a retirement HTTP route, server-side effect execution, remote `replace-file`, MCP enrollment, recovery import, and cross-host transfer.
- [x] The specification assigns schema and replay data to TASK-107.2, domain invariants to TASK-107.4, offline lifecycle composition to TASK-107.5, and authentication primitives to TASK-107.6 without exposing SQLite structures on the wire.
- [x] Contract section 20 is amended through section 15 to freeze this document by reference, section 17 records the amendment, and TASK-85 receives the matching summary comment.

## Definition of done

- [x] `mise run ci` passes on the final commit

## Plan

1. Inventory the real lease/read/watch/admin types, errors, and existing contract amendment rules.
2. Create the authoritative Backlog protocol document with exact HTTP routes, wire fields, bounds, auth/incarnation ordering, replay semantics, and operation exposure matrix.
3. Amend product contract sections 17 and 20 through section 15, add the required TASK-85 comment, and link the protocol document from TASK-107.1.
4. Review against every acceptance criterion, run repository quality gates, record evidence, and finalize the task.

## Notes

Created Remote Authority Protocol V1 (doc-4) and froze it from product-contract section 20 through a dated section 17 amendment. The protocol maps the existing lease, ledger, watch, reconciliation, and GC types plus named authentication/admin extensions; fixes routes, flags, bounds, envelopes, cancellation, replay ordering, auth precedence, error mapping, and exposure boundaries.

Independent review found six protocol ambiguities. Resolved all: GC is explicitly admin-only; remote exec has a 48 KiB intent bound and deterministic 192 KiB-per-stream capture within a 512 KiB completion receipt; operation renewal has a retained renewalId replay extension resolved before stale revision; authority mismatch is distinct from restore mismatch; invite expiry defaults to 15 minutes with 1 minute-24 hour bounds; and TASK-107.7 now requires the frozen --server-config flag.

Second review identified that component byte caps did not bound encoded containers. Resolved with a 1 MiB RemoteHandleV2 final-file limit, pre-begin full-handle fit check, 512 KiB final canonical completion receipt limit, encoded-string caps, deterministic aggregate trimming, and explicit maximum-resource plus escape-heavy conformance cases. Local handle and capture limits remain unchanged.

Validation: protocol acceptance assertion script passed all eight document/contract/comment checks; backlog doctor and git diff --check passed. Repository gates passed: mise run lint, format-check, test, typecheck, and final mise run ci (including staticcheck, go vet/test/race, govulncheck, E2E, and man generation). Independent adversarial review findings were fixed through two passes; final resumed review reported PASS with no validated findings.

## Summary

Froze Remote Authority Protocol V1 as the storage-neutral contract for worklease-http/1, including exact routes and CLI flags, typed request/result mappings and extensions, envelopes and bounds, authentication/incarnation/replay order, cancellation, cursor/watch behavior, error-to-HTTP mapping, and offline/client/HTTP/CLI/MCP boundaries. Amended product-contract sections 17 and 20 by reference, recorded the TASK-85 summary comment, and aligned TASK-107.7 on --server-config. Verified with protocol assertions, backlog doctor, git diff --check, all required repository gates, final mise run ci, and independent review with no remaining findings.

## Comments

### 2026-09-14T01:54:00Z @brett

Delivered protocol and contract changes in commit dd6a80f (Freeze remote authority protocol). Final review: PASS with no validated findings. Verification: mise run ci and required repository gates passed.
