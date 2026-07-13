---
id: doc-1
title: Worklease Workflow
type: guide
created_date: '2026-07-13 19:42'
updated_date: '2026-07-13 22:03'
tags:
  - agent
  - workflow
  - provider-neutral
---
# Worklease Workflow

This guide is the human-facing entry point for the provider-neutral backlog coordination skill at [`skills/worklease-workflow/SKILL.md`](../../../../skills/worklease-workflow/SKILL.md).

## When to use it

Use the skill when work needs more than a task status: dependency-aware selection, bounded ownership, heartbeats, durable progress checkpoints, review boundaries, handoff, or archive. It is useful for coding agents, operators, and other callers that can provide the underlying work system.

For ordinary Backlog.md task lifecycle changes, keep using the `backlog` CLI and its built-in guides. This skill does not replace `backlog task`, does not write task files, and does not choose a provider.

## Start here

Read the project guidance and current backlog state before acting:

```bash
mise exec -- backlog instructions overview
mise exec -- backlog search "<terms>" --plain
mise exec -- backlog task list --plain
```

Then load the generic skill. The caller must supply an implementation of its capability boundary:

- source resolution and discovery;
- item reads and durable writes;
- dependency/status/priority mapping;
- one exact opaque claim resource per target and operation scope;
- claim inspection, acquisition, heartbeat, and release authority; and
- review and archive operations when supported.

The skill treats source locators, item IDs, statuses, metadata, claim resources, and receipts as opaque caller-owned values. It deliberately contains no provider detection, resource derivation, provider commands, credential rules, or remote-fencing claims. When caller context does not already provide those mappings, load a provider-specific adapter after the generic contract and only for the providers actually resolved. The adapter inherits generic graph construction and selection; it records `providerMutationFenced: false` unless the provider mutation itself supplies fencing evidence.

## Operating loop

1. Resolve the caller's ordered sources and selectors.
2. Discover the complete scoped item set, including dependencies needed for readiness checks.
3. Build the dependency graph before selecting work; report missing or cyclic dependencies instead of guessing.
4. Select one item or a dependency-ready wave, excluding active claims and terminal work.
5. Accept one exact caller-supplied claim resource and atomically claim it before delegation or edits.
6. Retain the resource, claim ID, token, revision, expiry, and guarantee; record the caller-declared guarantee scope alongside the receipt.
7. Heartbeat before the lease is half-expired and around long work.
8. Re-read eligibility, ownership, and provider state before each durable mutation.
9. Persist and verify a coherent provider checkpoint before release or handoff.
10. Release only the matching claim after retaining the provider receipt or verifying source state.
11. Review or archive only when the caller explicitly authorizes those boundaries.

Checkpoint-before-release is caller policy. Worklease validates ownership and a non-blank audit reason; the reason is not provider-checkpoint proof.

If the caller cannot provide one capability, return a structured capability result. Never invent a resource mapping, assignee/status/comment lock, writable local shadow, or provider fencing guarantee. Worklease can fence its matching guarded local operation among cooperating same-host callers; it does not make an invoked provider CLI/API mutation provider-fenced or exclude cross-host workers. Never put the bearer token in status output, diagnostics, provider checkpoints, logs, or handoffs.

## Source of truth

The caller's backing system remains authoritative for item content and workflow state. A Worklease claim or operation receipt is coordination evidence, not a provider checkpoint. For Backlog.md changes specifically, an authorized caller or adapter uses `backlog task view`, `backlog task edit`, `backlog doc create/update`, and the matching built-in guide rather than editing `docs/backlog/` records directly.
