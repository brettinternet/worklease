---
id: DRAFT-12
title: Evaluate resource-scoped coordination signals
status: Draft
assignee: []
created_date: '2026-09-18 20:22'
updated_date: '2026-09-18 20:22'
labels:
  - coordination
  - design
dependencies: []
references:
  - docs/claim-model.md
  - docs/cli-reference.md
  - skills/worklease-workflow/SKILL.md
type: spike
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Recommendation

Do not add general agent-to-agent messaging to Worklease. Preserve Worklease as a claim authority rather than expanding it into a chat service or message broker.

Investigate a narrower feature only if concrete workflows cannot be expressed through the existing event and watch APIs: typed, resource-scoped coordination signals delivered through the ordered lifecycle event stream.

## Motivation

Worklease already provides the useful real-time substrate: ordered events, durable authority-bound cursors, long-polling watch, claims, heartbeats, checkpoints, transfers, and release reasons. Agents may still need to wake another coordinator for a small number of explicit intents such as requesting release or announcing that a handoff is ready.

General messaging would introduce inboxes, agent addressing, delivery acknowledgments, privacy, retention, backpressure, and identity semantics. That does not fit the current model, where agentId is audit metadata rather than authorization and provider comments or checkpoints remain the durable handoff mechanism.

## Proposed direction

Before adding server behavior, prototype orchestration using events and watch and document the workflows that remain impossible or unsafe. If a product gap remains, consider an MVP with these constraints:

- Signals address one or more exact resources, never an agent identity.
- Signal kinds come from a small closed set, initially candidates such as release-requested, takeover-requested, handoff-ready, review-requested, and blocked.
- Payloads are bounded, structured, non-secret public metadata; arbitrary message bodies are excluded.
- Signals use the existing ordered event sequence and cursor/watch behavior. Delivery is at-least-once, and consumers deduplicate by event sequence.
- Signals are coordination hints only. They do not transfer ownership, renew a claim, prove provider progress, establish fencing, or authorize provider writes.
- Local and remote authorities expose the same semantics, including explicit retention gaps and authority/restore cursor binding.
- Authorization, rate limiting, retention, and visibility are specified before implementation.

## Non-goals

- Direct messages or per-agent inboxes
- Chat threads or arbitrary conversation
- Secret transport
- Exactly-once delivery
- Replacing provider comments, task state, or durable checkpoints
- Treating agentId as an authorization principal

## Questions to answer

1. Which real workflow cannot be handled by watching claim lifecycle events today?
2. Must a sender hold the resource claim, or should another enrolled writer be able to request an action from the holder?
3. Which signal kinds have unambiguous machine behavior and do not duplicate provider workflow?
4. Should signals exist only as retained events, or require current state and explicit resolution?
5. What abuse and backpressure limits are necessary for remote authorities?
6. Can the need be met entirely by a client-side orchestration helper over watch?

## Decision threshold

Proceed with implementation only when at least one concrete multi-agent workflow demonstrates that lifecycle events plus client-side orchestration are insufficient. Prefer improving watch ergonomics over adding a new durable protocol surface.
<!-- SECTION:DESCRIPTION:END -->
