---
name: worklease-workflow
description: Coordinate generic work items with dependency-aware selection, bounded claims, durable progress, review boundaries, and archive operations without assuming a backend or provider.
disable-model-invocation: true
---

# Worklease Workflow

Use this skill when a caller needs a safe work loop around an arbitrary backlog, queue, issue system, document set, or other work source. It is a coordination contract, not an integration. The caller remains responsible for connecting the contract to its own source, authentication, mutation APIs, and claim authority.

This skill does not discover, name, select, or configure providers. It does not prescribe a command, storage engine, credential model, status vocabulary, transport, or fencing implementation. Source locators, item IDs, statuses, metadata, and receipts are opaque caller-owned values.

## Progressive loading

1. Read [`references/contract.md`](references/contract.md) before designing or executing a workflow.
2. Declare the caller's capability object: source resolution, discovery, reads, writes, claim authority, and any review/archive operations.
3. Map the caller's source to the normalized contract without leaking backend assumptions into scheduling logic.
4. Use only capabilities the caller explicitly authorizes. A missing capability is a structured result, never an invented fallback.

## Responsibility boundary

The caller owns command scope, authority, source-specific interpretation, and durable writes. This skill owns the coordination invariants:

- resolve all explicit sources and selectors in caller order;
- discover the complete dependency graph before selecting work;
- select only terminal-prerequisite, unblocked, claimable work;
- claim before delegation, isolation, or edits;
- heartbeat bounded leases while work is active;
- revalidate eligibility and ownership before durable mutations;
- checkpoint durable progress before release or handoff; and
- keep review and archive boundaries explicit.

A caller may use a local lease or lock, but it must report the guarantee it actually has. Same-host coordination among cooperating callers is not cross-host exclusion and is not source-side mutation fencing.

## Normalized operating loop

1. Resolve the ordered source and item arguments.
2. Discover all scoped items and any dependencies required to establish readiness.
3. Build and validate the dependency graph. Missing references and cycles block the affected work.
4. Return `complete`, `blocked`, or `active-claims` when no item can be selected; do not skip a gate to manufacture work.
5. Select one item or a dependency-ready wave.
6. Acquire a fresh claim with a unique ownership epoch before handing off or editing.
7. Revalidate dependencies and claim ownership immediately before each durable write.
8. Heartbeat before half the lease elapses and around long-running operations.
9. Record a durable checkpoint through the caller's write capability.
10. Review or archive only at an explicit authorized boundary.
11. Release only the exact current claim after the checkpoint succeeds.

On interruption, let the bounded claim expire or perform an explicit coherent handoff. A resumed attempt receives a fresh claim ID and token; it never adopts an unexpired claim merely because the agent identity is unchanged.

## Safe failure behavior

Return structured diagnostics for `blocked`, `active-claims`, `conflict`, `ambiguous`, `ineligible`, `capability`, and `complete`. Preserve source and selector order in both success and failure results. Stop when ownership, eligibility, a durable write, or a receipt is uncertain. Never use an assignee, status, comment, branch, worktree, ordinary lock file, or local writable shadow as a substitute for a claim or durable checkpoint.

## Human and agent use

Humans can use this contract to review a work system's coordination guarantees and to implement a small adapter without changing scheduling rules. Agents should load the contract before acting, keep the caller's task system authoritative, show the claim/guarantee they hold, and leave durable progress where the caller can discover it. The skill does not replace the caller's own task commands or permit direct edits to a backing source.
