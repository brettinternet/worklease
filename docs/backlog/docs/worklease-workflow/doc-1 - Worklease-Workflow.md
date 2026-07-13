---
id: doc-1
title: Worklease Workflow
type: guide
created_date: '2026-07-13 19:42'
updated_date: '2026-07-13 20:01'
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
- claim, heartbeat, and release authority; and
- review and archive operations when supported.

The skill treats source locators, item IDs, statuses, metadata, and provider receipts as opaque values. It deliberately contains no provider detection, provider commands, credential rules, or remote-fencing claims.

## Operating loop

1. Resolve the caller's ordered sources and selectors.
2. Discover the complete scoped item set, including dependencies needed for readiness checks.
3. Build the dependency graph before selecting work; report missing or cyclic dependencies instead of guessing.
4. Select one item or a dependency-ready wave, excluding active claims and terminal work.
5. Atomically claim before delegation or edits; retain the claim ID, token, revision, and expiry.
6. Heartbeat before the lease is half-expired and around long work.
7. Re-read eligibility before each durable mutation; write progress through the caller's authority.
8. Persist a coherent checkpoint before release or handoff.
9. Revalidate the claim, checkpoint, and provider receipt; release only the matching claim.
10. Review or archive only when the caller explicitly authorizes those boundaries.

If the caller cannot provide one capability, return a structured capability result. Never invent an assignee/status/comment lock, writable local shadow, or provider fencing guarantee. A same-host lease may coordinate cooperating callers, but it is not cross-host or provider-side exclusion unless the caller proves that stronger authority.

## Source of truth

The caller's backing system remains authoritative for item content and workflow state. A local lease store, if provided by the caller, is coordination state only. For Backlog.md changes specifically, use `backlog task view`, `backlog task edit`, `backlog doc create/update`, and the matching built-in guide rather than editing `docs/backlog/` records directly.
