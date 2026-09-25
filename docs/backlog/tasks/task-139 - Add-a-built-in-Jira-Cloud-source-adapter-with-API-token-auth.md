---
id: TASK-139
title: Add a built-in Jira Cloud source adapter with API-token auth
status: To Do
assignee: []
created_date: '2026-09-25 16:21'
updated_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.2
documentation:
  - docs/work-queue-tui-proposal.md
  - skills/worklease-workflow/references/source-providers/jira.md
priority: medium
type: feature
ordinal: 61000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The user asked for Jira support on 2026-09-25 during TASK-134 (S8 intake), making Jira an accepted S8 candidate alongside Linear. Teams that plan in Jira cannot see its issues in `worklease queue` today, so agents and humans there get no readiness, dependency, or claim view without leaving the tool.

Scope is Jira Cloud authenticated with Atlassian API tokens (email plus token over Basic auth, or scoped API tokens), supplied through the user-configured credential helper introduced with the Linear adapter. OAuth is explored separately in DRAFT-14. Jira Data Center differs in user identity (username vs accountId), comment format (wiki markup vs ADF), search endpoints, and auth; it is out of scope here and needs its own intake decision.

Build it as a built-in adapter, following the Linear decision in TASK-134, and structure it like the Linear parent (probe, identity vectors, read adapter, sync, claims, then focused writes); split into subtasks when picked up.

Known Jira constraints to account for (verify with the probe):
- Issue search moved to `POST /rest/api/3/search/jql` with `nextPageToken`/`isLast` and no exact total; `/rest/api/3/search` with `startAt`/`total` is deprecated and being removed. `approximate-count` only estimates.
- Issue keys change when an issue moves projects; the numeric issue ID does not. The skill's jira.md currently puts the project/filter locator in the `generic` source, which would give a moved issue a new claim key; resolve that explicitly.
- Workflows, statuses, and link-type names are configured per site and project. `statusCategory` (new/indeterminate/done) and `resolution` are the normalized signals; a done status with a non-success resolution is not successful completion (D27).
- JQL `updated` has minute resolution, deleted issues disappear from search, and webhooks need admin or app installation.
- Comments are ADF (no hidden HTML comment for the D7 operation marker); assignment is single-valued by accountId; writes are unconditional.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 docs/work-queue-tui-proposal.md records the Jira decision in §2 and §16, a Jira Cloud evidence subsection in §3 from a live probe on a user-approved test project (synthetic issues only, cleaned up), and a Jira column in the §7 declarations table.
- [ ] #2 The probe establishes search pagination and ordering, whether link add/remove changes `updated` on either issue, project-move identity behavior, deleted and permission-lost visibility, transition field requirements, rate-limit responses and headers, ADF comment round-trip for the operation marker, and absence of native claims.
- [ ] #3 A Jira Cloud source is configured in queue.yaml with an explicit site, JQL scope (project or saved filter), account, and credential helper; the token is held only in memory, never persisted or logged, and the authenticated accountId is verified against the configured account before every write and whenever credentials change.
- [ ] #4 Claim keys use the existing static `generic` policy with a site-qualified source and a stable issue identity chosen from probe evidence; identity vectors pin the bytes, queue- and CLI-derived keys are byte-equal, and the skill's jira.md matches the chosen form.
- [ ] #5 A project move, key change, or site mismatch is detected and handled per §6: claims stay correct or are disabled until an explicit rebind, never silently rekeyed.
- [ ] #6 The read adapter lists the full configured scope with coverage that reports totals as estimated or unknown, hydrates items and links, maps user-configured link types to hard prerequisites (hierarchy and other links informational), and keeps raw status beside the normalized category and resolution.
- [ ] #7 Incremental sync and reconciliation work with the queue index and per-quota scheduler; a partial scan never advances the watermark or proves deletion, and a missing issue is not treated as deleted without reconciliation.
- [ ] #8 Claim for me, the D11 check, the identity gate, `queue next --claim`, and MCP `queue_next` work on Jira sources, and remote admission accepts the keys under `coordination:`.
- [ ] #9 Focused writes ship after reads are proven: configured transitions (including Start work) using only transitions discovered for that issue, a comment carrying a verifiable operation marker, and assign-to-me with declared single-assignee replace semantics and explicit confirmation when someone else is assigned; each follows the §8 recovery pipeline with read-back.
- [ ] #10 The adapter passes the shared adapter conformance suite, and tests cover pagination interruption, rate limiting with retry time, permission loss, project moves, non-success resolutions, and secret redaction.
- [ ] #11 User-facing queue docs describe Jira Cloud setup, the required token scopes, and the Data Center exclusion.
<!-- AC:END -->
