# Proposal: provider-backed work queue

**Status:** Idea for exploration; not an implemented Worklease feature.

## Goal

Give humans and coding agents one fast, Vim-first TUI for finding and claiming work without moving their tasks into another tracker. Make the *work queue* source-pluggable: start with Backlog.md, but leave room for Beads, GitHub Issues, Linear, Jira, and other sources without changing Worklease's authority.

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

A source adapter supplies stable item identity, readable work and dependencies, and—only when explicitly authorized—provider updates with verifiable receipts. The queue derives exact claim resources from the selected source and item according to a documented policy. Keep a small, capability-based adapter boundary from the start; avoid assuming that every source supports dependencies, assignment, conditional writes, or the same definition of “ready.” An out-of-process, versioned JSON protocol is one possible route to third-party adapters, but its shape should be exercised by more than one real source before being fixed.

## First workflow

1. Browse and search Backlog.md tasks quickly, including long descriptions; show dependency readiness alongside current claim holder and expiry. Make Vim navigation consistent throughout the TUI. Offer a machine-readable query path for agents.
2. Select an eligible item and acquire its exact task resource (and any explicitly required resources) through Worklease. Surface contention; never interpret assignment or status as an exclusive claim.
3. Re-read provider state before an authorized status or progress update, then verify the provider's durable result independently. Claim acquisition alone must **not** mark a task In Progress.
4. Heartbeat while working; before release, verify the provider checkpoint. If ownership, an external operation, or the provider result is uncertain, stop and expose recovery rather than retrying blindly.

A claim coordinates cooperating workers, not uncooperative provider writers. Worklease expiry ends authorization but does not prove a prior worker stopped. Local Backlog.md/path resource keys are host-local; a cross-host setup needs deliberately chosen portable identities and an appropriate remote authority. Neither local nor remote claims by themselves fence provider writes; see [claim guarantees](claim-model.md#guarantees).

## Possible sources and authentication

| Source | Possible first integration | Authentication considerations |
| --- | --- | --- |
| Backlog.md / local Markdown | Local CLI or files, with provider-authorized writes | No remote login; preserve the source's task format and write rules. |
| Beads | `bd` JSON CLI | Local use need not add another login. Beads also has its own assignment/claim flow: do not mistake that for an atomic Worklease claim or silently maintain two claim authorities. |
| GitHub Issues | `gh` CLI | Reuse the user's `gh` login initially; a distributed app may need a GitHub App and scoped installation permissions. |
| GitLab | `glab` CLI or API | Reuse an existing login if possible; account for instance-specific access and scopes. |
| Linear | GraphQL API | Personal API key suits a personal integration; distribution to other users calls for OAuth and token lifecycle management. |
| Jira Cloud | REST API | Personal API tokens are possible; OAuth, site discovery, permissions, and differing Jira deployments make a general adapter harder. |
| Azure DevOps or custom sources | Only when a real user needs them | Prefer existing credentials or a source-owned credential helper; document identity and authorization guarantees per adapter. |

Authentication belongs to the queue or source adapter, not the Worklease authority. Do not put provider secrets in resource keys, Worklease metadata, logs, or handoffs. Reading tasks is a smaller commitment than mutating them: write capability needs explicit permission, freshness checks, and verified provider receipts. Reusing an installed CLI can simplify an initial personal workflow but should not be assumed to provide universal authentication or stable schemas.

## Scope and open questions

Prototype as an optional companion surface, not a new store or provider plugin inside Worklease. Start read-only with Backlog.md and claim visibility; add claim actions and provider updates only after the identity and failure paths are clear. Benchmark startup, search, and navigation on large task corpora before choosing an index or runtime. A Go rewrite is not itself a performance strategy if each query still reparses and searches every full task body.

Questions for a future implementer:

- Which second source best tests the adapter boundary: Beads' local CLI, GitHub's hosted API, or another real user need? What capabilities must be optional?
- How are source instance and item identity mapped to collision-resistant, exact Worklease resource keys? Which identities are portable across hosts, and how is authority choice surfaced?
- Should claim acquisition and provider updates remain separate actions in the TUI, and how should partial success or uncertain outcomes be recovered?
- When is a versioned out-of-process adapter protocol justified, and how will extensions receive credentials without exposing them to Worklease?
- Does the proven UI belong in an optional Worklease command or a separate binary? Keep the workflow usable without a web server either way.

The prototype should answer these questions rather than pre-commit Worklease to an issue store, a universal task schema, or a plugin runtime inside its authority.
