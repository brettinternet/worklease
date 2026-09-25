---
id: TASK-141
title: Add a built-in Beads source adapter
status: In Progress
assignee:
  - '@pi-task-141'
created_date: '2026-09-25 16:26'
updated_date: '2026-09-25 16:42'
labels:
  - work-queue
milestone: m-1
dependencies: []
documentation:
  - docs/work-queue-tui-proposal.md
  - skills/worklease-workflow/references/source-providers/backlog-md.md
priority: medium
type: feature
ordinal: 63000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Beads (the `bd` CLI) is a Git-backed issue tracker designed for coding agents, with typed dependencies (such as blocks, parent-child, related, discovered-from) and a built-in ready-work query. Agent workflows that plan in Beads cannot use `worklease queue` for readiness, cross-source views, or claims today. The user asked for Beads support on 2026-09-25 during TASK-134 (S8 intake).

Beads is local and CLI-driven like Backlog.md, so the built-in Backlog.md adapter (D13) is the closest model: structured CLI output only, one explicit checkout, and declared Git side effects. Details below come from memory, not a probe, and `bd` is not installed on the reference machine; the probe must confirm or correct them:
- Storage is a local database plus a Git-tracked JSONL export, and a background daemon may flush, commit, or sync automatically. Reads and writes may therefore have Git side effects that need the same disclosure and consent as Backlog.md (§5).
- Issue IDs may be hash-based to avoid collisions across clones; if they can still collide or be renamed, the D12 duplicate-ID and renumber-as-migration rules apply.
- Beads has its own in-progress status and assignee; those are visibility, never a Worklease claim (D5).

No `beads` resource policy is built in. Portable coordination uses the existing `generic` policy with an explicit agreed source binding, as D12 does for Backlog.md. Adding a host-local Beads policy would be a resource-policy change requiring its own decision.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 docs/work-queue-tui-proposal.md records the decision in §2 and §16, a §3 evidence subsection from a scale and side-effect probe of a pinned `bd` version on disposable projects (list and show cost at 102, 1,000, and 10,000 issues; JSON schemas; dependency fields in bulk output; daemon, commit, hook, and remote behavior on reads and writes; ID generation and collision behavior across clones), and a Beads column in the §7 declarations table.
- [ ] #2 The adapter uses only documented `bd` JSON output and documented write flags, pins or checks the supported version, and reports a structured diagnostic on version drift or unparseable output.
- [ ] #3 A Beads source is configured in queue.yaml with one explicit checkout; reads that can contact remotes or trigger commits are declared effects requiring the same explicit consent as Backlog.md remote operations, and a local-only source makes no network access.
- [ ] #4 Claims use the existing `generic` policy through an explicit portable source binding confirmed by the identity gate; identity vectors pin the keys, queue- and CLI-derived keys are byte-equal, and duplicate or renamed IDs disable claims per D12.
- [ ] #5 Beads blocking dependencies become hard prerequisites with the configured completion condition; parent-child, related, and discovered-from relations are informational unless the probe and a recorded decision say otherwise. Beads' own ready result is shown as provider-reported beside queue readiness.
- [ ] #6 Beads in-progress status and assignee are displayed as provider state and assignment, never as claims or claim availability.
- [ ] #7 Change detection keeps the index fresh without a process per issue on every refresh, using bulk output or a probed invalidation signal; a partial read never proves deletion.
- [ ] #8 Claim for me, the D11 check, `queue next --claim`, and MCP `queue_next` work on Beads sources.
- [ ] #9 Focused writes (state, progress note or comment with operation marker, assign-to-me) ship after reads are proven, follow the §8 recovery pipeline, disclose Git effects in previews, and verify read-back including any commit.
- [ ] #10 The adapter passes the shared adapter conformance suite; tests cover missing or incompatible `bd`, daemon-induced concurrent changes, duplicate IDs, dependency cycles, and unrelated staged changes staying staged.
- [ ] #11 User-facing queue docs describe Beads setup, the portable binding, and declared side effects.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Probe pinned Beads 1.3.0 on disposable projects for structured API, identity, scale and side effects; record evidence and decisions.
2. Add built-in Beads read adapter, configuration, portable identity and graph handling with focused conformance tests.
3. Add recoverable writes and CLI/MCP integration, user docs and tests.
4. Run focused and repository gates, review, commit/merge in worktree, finalize task and clean up.
<!-- SECTION:PLAN:END -->
