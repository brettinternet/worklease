---
name: worklease-source-workflow
description: Map caller-selected backlog, issue, document, or custom work sources onto the provider-neutral Worklease workflow without duplicating scheduling or claim semantics.
disable-model-invocation: true
---

# Worklease Source Workflow

Use this companion when a caller needs to connect a concrete work source—Backlog.md, loose Markdown, GitHub Issues, Linear, Jira, or a custom system—to Worklease coordination. It is a provider composition contract, not a provider client, scheduler, credential source, or mutation authority.

## Progressive loading

1. Read [`../worklease-workflow/references/contract.md`](../worklease-workflow/references/contract.md) first. It is the sole normative contract for graph construction, selection, claims, heartbeat/release sequencing, checkpoint ordering, review defaults, and structured results.
2. Read [`references/provider-contract.md`](references/provider-contract.md) to implement the source/provider capability boundary.
3. Resolve only caller-explicit or caller-configured provider kinds. Preserve source and selector order; never choose whichever integration responds first.
4. Load exactly one matching provider reference per resolved kind from [`references/providers/index.md`](references/providers/index.md). Load [`references/providers/unknown.md`](references/providers/unknown.md) when no dedicated mapping exists.
5. Use [`references/provider-authoring-checklist.md`](references/provider-authoring-checklist.md) before claiming a custom adapter complete. Load an [`example`](examples/index.md) only when its guarantee shape matches the caller's mutation path.

## Layer boundary

`worklease-workflow` owns all provider-independent invariants. This skill maps provider values and operations into that contract. A provider reference may define resolution, discovery, normalized fields, resource policy, authoritative mutations, receipts, review boundaries, and archive behavior. It must not reimplement dependency scheduling, active-claim filtering, tie-breakers, ownership epochs, heartbeat cadence, checkpoint-before-release, or generic result vocabulary.

The bundled `worklease.adapters` are deterministic resource-key and local-capability policies after the caller supplies a provider, source, and item. They do not discover provider work, authenticate, execute provider writes, or prove provider-side fencing. A source workflow adapter may use a bundled key adapter; it still owns the provider reads, writes, and receipts authorized by the caller.

## Required composition

For every resolved source, the caller or adapter must provide:

- an unambiguous `Source` identity and ordered discovery operation;
- source-qualified `WorkRef` values and normalized `WorkItem` state;
- complete dependencies and caller-owned terminal/blocked interpretation;
- one exact Worklease resource and declared local capability for each claim scope;
- authoritative item refresh, mutation, and durable checkpoint operations;
- provider version or conditional-write evidence when available;
- explicit review-boundary and archive capabilities, or structured `capability` results; and
- provider receipts that can be re-read from the authoritative source.

Use the generic workflow only after those capabilities are declared. Missing reads, writes, canonical identity, or receipts are `capability` failures; they are never permission to invent a local shadow backlog or weaker mutation path.

## Guarantee mapping

Default `providerMutationFenced` to `false`. A Worklease `fenced` claim covers only its named same-host guarded local operation. A provider CLI or remote API invoked from a guarded local process remains `local-coordination` unless the durable provider mutation itself uses provider-side compare-and-set/fencing and returns evidence.

A loose-Markdown `replace-file` path may report fencing only for the exact source file mutation guarded by the matching source claim and expected SHA-256. Assignment, status, comment, branch, worktree, local receipt, pre/post read, or provider timestamp without conditional enforcement is not provider fencing.

## Safe provider operation

Refresh the exact `WorkRef`, dependencies, claim, and provider version before a durable mutation. Preserve unrelated provider fields. Retain both the Worklease operation receipt and the provider receipt, but treat only the verified provider state as the checkpoint. On ambiguity, ownership loss, version conflict, unsupported capability, or missing receipt, stop without release or further mutation.

Never expose the claim token in provider comments, status, checkpoints, logs, diagnostics, examples, or handoffs. The token is passed only to claim mutations and guarded local operations.
