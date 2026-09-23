# Proposal: provider-backed work queue

**Status:** Idea for exploration; not an implemented Worklease feature.

## Goal

Give humans and coding agents one fast, Vim-first TUI for finding and claiming work without moving their tasks into another tracker. Start with Backlog.md; consider GitHub Issues, Linear, and other sources only after the first workflow proves useful.

The distinction from an agent issue tracker such as Beads is **coordination across existing sources and resources**, not a new task database or another `ready` command. A view can combine provider readiness with live Worklease occupancy: a task may be unblocked in the backlog but unavailable because another worker holds its claim or a required resource. It should explain both.

## Boundary

```text
Backlog.md / future provider ── tasks, dependencies, status, durable updates
             │
             ▼
      Work queue TUI/API       ── discovery, presentation, provider adapter
             │
             ▼
      Worklease authority      ── claims, expiry, conflicts, history
```

The queue is a **client of both** the task source and Worklease. The source remains authoritative for task content and progress. Worklease remains provider-neutral and owns neither discovery nor task mutations. A disposable local index may accelerate browsing, but must not become a second source of truth. The queue can initially call existing versioned JSON CLI surfaces rather than add a plugin runtime to the authority.

A source adapter supplies stable item identity, readable work and dependencies, and—only when explicitly authorized—provider updates with verifiable receipts. The queue derives exact claim resources from the selected source and item according to a documented policy. Do not generalize the adapter interface until a second source exercises it.

## First workflow

1. Browse and search Backlog.md tasks quickly, including long descriptions; show dependency readiness alongside current claim holder and expiry. Make Vim navigation consistent throughout the TUI. Offer a machine-readable query path for agents.
2. Select an eligible item and acquire its exact task resource (and any explicitly required resources) through Worklease. Surface contention; never interpret assignment or status as an exclusive claim.
3. Re-read provider state before an authorized status or progress update, then verify the provider's durable result independently. Claim acquisition alone must **not** mark a task In Progress.
4. Heartbeat while working; before release, verify the provider checkpoint. If ownership, an external operation, or the provider result is uncertain, stop and expose recovery rather than retrying blindly.

A claim coordinates cooperating workers, not uncooperative provider writers. Worklease expiry ends authorization but does not prove a prior worker stopped. Local Backlog.md/path resource keys are host-local; a cross-host setup needs deliberately chosen portable identities and an appropriate remote authority. Neither local nor remote claims by themselves fence provider writes; see [claim guarantees](claim-model.md#guarantees).

## Scope and validation

Prototype as an optional companion surface, not a new store or provider plugin inside Worklease. Start read-only with Backlog.md and claim visibility; add claim actions and guarded provider updates only after the identity and failure paths are clear. Benchmark startup, search, and navigation on large task corpora before choosing an index or runtime. A Go rewrite is not itself a performance strategy if each query still reparses and searches every full task body.

Decide after the prototype whether the UI belongs as an optional Worklease command or a separate binary. Either way, preserve the authority boundary and allow the workflow to work without a web server. Later source adapters should be justified by real provider-specific needs, especially identity, conditional updates, and receipt verification—not by a generic plugin framework built in advance.
